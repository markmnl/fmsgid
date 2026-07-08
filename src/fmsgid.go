package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/joho/godotenv"
	"golang.org/x/text/cases"
)

var pool *pgxpool.Pool

func init() {
	// Load .env file if present (ignore error if not found)
	_ = godotenv.Load()

	// Set GIN_MODE from env if not already set
	if mode := os.Getenv("GIN_MODE"); mode != "" {
		gin.SetMode(mode)
	}
}

const (
	TypeRecv = 1
	TypeSend = 2
)

type AddressTx struct {
	Address   string  `json:"address"`
	Timestamp float64 `json:"ts"`
	Size      int     `json:"size"`
}

type CreateAddressReq struct {
	Address     string `json:"address"`
	DisplayName string `json:"display_name"`
}

type AddressDetail struct {
	Address             string   `json:"address"`
	DisplayName         string   `json:"displayName"`
	AcceptingNew        bool     `json:"acceptingNew"`
	LimitRecvSizeTotal  int64    `json:"limitRecvSizeTotal"`
	LimitRecvSizePerMsg int64    `json:"limitRecvSizePerMsg"`
	LimitRecvSizePer1d  int64    `json:"limitRecvSizePer1d"`
	LimitRecvCountPer1d int64    `json:"limitRecvCountPer1d"`
	LimitSendSizeTotal  int64    `json:"limitSendSizeTotal"`
	LimitSendSizePerMsg int64    `json:"limitSendSizePerMsg"`
	LimitSendSizePer1d  int64    `json:"limitSendSizePer1d"`
	LimitSendCountPer1d int64    `json:"limitSendCountPer1d"`
	RecvSizeTotal       int64    `json:"recvSizeTotal"`
	RecvSizePer1d       int64    `json:"recvSizePer1d"`
	RecvCountPer1d      int64    `json:"recvCountPer1d"`
	SendSizeTotal       int64    `json:"sendSizeTotal"`
	SendSizePer1d       int64    `json:"sendSizePer1d"`
	SendCountPer1d      int64    `json:"sendCountPer1d"`
	Tags                []string `json:"tags"`
}

func initPool() error {
	var err error
	pool, err = pgxpool.Connect(context.Background(), "")
	if err != nil {
		return err
	}
	return pool.Ping(context.Background())
}

// isValidFmsgAddr reports whether addr is in fmsg format: @user@example.com
func isValidFmsgAddr(addr string) bool {
	if len(addr) < 3 || addr[0] != '@' {
		return false
	}
	return strings.Count(addr, "@") == 2
}

func getAddressDetail(c *gin.Context) {
	ctx := c.Request.Context()

	// TODO move data to body to hide
	addr, hasAddr := c.Params.Get("address")
	if !hasAddr {
		c.AbortWithStatus(400)
		return
	}

	if !isValidFmsgAddr(addr) {
		c.AbortWithStatus(400)
		return
	}

	// collapse address using Unicode case folding
	addr = cases.Fold().String(addr)

	rows, err := pool.Query(ctx, sqlSelectAddressDetail, addr)
	if err != nil {
		c.AbortWithError(500, err)
		return
	}
	defer rows.Close()

	if !rows.Next() {
		// address not found
		c.AbortWithStatus(404)
		return
	}

	var ad AddressDetail

	err = rows.Scan(&ad.Address, &ad.DisplayName, &ad.AcceptingNew, &ad.LimitRecvSizeTotal,
		&ad.LimitRecvSizePerMsg, &ad.LimitRecvSizePer1d, &ad.LimitRecvCountPer1d,
		&ad.LimitSendSizeTotal, &ad.LimitSendSizePerMsg, &ad.LimitSendSizePer1d,
		&ad.LimitSendCountPer1d)
	if err != nil {
		c.AbortWithError(500, err)
		return
	}

	// select actuals
	rows, err = pool.Query(ctx, sqlActuals, addr)
	if err != nil {
		c.AbortWithError(500, err)
		return
	}
	defer rows.Close()

	// no rows is possible when haven't sent or recieved anything!
	if rows.Next() {
		err = rows.Scan(&ad.SendSizeTotal, &ad.SendCountPer1d, &ad.SendSizePer1d,
			&ad.RecvSizeTotal, &ad.RecvCountPer1d, &ad.RecvSizePer1d)
		if err != nil {
			c.AbortWithError(500, err)
			return
		}
	}

	c.JSON(http.StatusOK, ad)
}

// postCreateAddress registers a new address with default quotas, idempotently.
// It never modifies an address that already exists — use CSV sync to change
// quotas or accepting_new on an existing address.
func postCreateAddress(c *gin.Context) {
	ctx := c.Request.Context()

	var req CreateAddressReq
	if err := c.BindJSON(&req); err != nil {
		log.Printf("WARN: Parsing CreateAddressReq: %s\n", err)
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	if !isValidFmsgAddr(req.Address) {
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	addrLower := cases.Fold().String(req.Address)

	tag, err := pool.Exec(ctx, sqlInsertAddressIfNotExists, addrLower, req.Address, req.DisplayName)
	if err != nil {
		c.AbortWithError(500, err)
		return
	}

	if tag.RowsAffected() == 0 {
		c.Status(http.StatusOK)
		return
	}
	c.Status(http.StatusCreated)
}

func postAddressTxSend(c *gin.Context) {
	postAddressTx(c, TypeSend)
}

func postAddressTxRecv(c *gin.Context) {
	postAddressTx(c, TypeRecv)
}

func postAddressTx(c *gin.Context, typ int) {
	ctx := c.Request.Context()

	var tx AddressTx
	err := c.BindJSON(&tx)
	if err != nil {
		log.Printf("WARN: Parsing AddressTx: %s\n", err)
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	_, err = pool.Exec(ctx, sqlInsertTx, tx.Address, tx.Timestamp, typ, tx.Size)
	if err != nil {
		c.AbortWithError(500, err)
		return
	}

}

func main() {
	log.SetPrefix("fmsgid: ")
	err := initPool()
	if err != nil {
		log.Fatalf("ERROR: Failed to connect to database: %s", err)
	}
	defer pool.Close()
	log.Println("INFO: Database initialized")

	// Start CSV sync if configured
	csvFile := os.Getenv("FMSGID_CSV_FILE")
	if csvFile != "" {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go startCSVWatcher(ctx, pool, csvFile)
	}

	port := os.Getenv("FMSGID_PORT")
	if port == "" {
		port = "8080"
	}
	r := gin.Default()
	r.GET("/fmsgid/:address", getAddressDetail)
	r.POST("/fmsgid", postCreateAddress)
	r.POST("/fmsgid/send", postAddressTxSend)
	r.POST("/fmsgid/recv", postAddressTxRecv)
	err = r.Run(":" + port)
	if err != nil {
		log.Fatalf("ERROR: Running gin engine: %s", err)
	}
}
