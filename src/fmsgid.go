package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v4"
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
	// A release gives back the storage of a message deleted for good: it
	// takes its size off the total, but not off the daily usage, which
	// counts what was sent and received that day.
	TypeRecvRelease = 3
	TypeSendRelease = 4
)

type AddressTx struct {
	Address   string  `json:"address"`
	Timestamp float64 `json:"ts"`
	Size      int     `json:"size"`
}

type CreateAddressReq struct {
	Address     string `json:"address"`
	DisplayName string `json:"display_name"`
	// AcceptingNew, when given, is set on the address whether it is new or
	// already exists. When omitted a new address takes the column default and
	// an existing one is left as it is.
	AcceptingNew *bool `json:"accepting_new"`
	// QuotaPool, when given, puts the address in the quota pool of the
	// address it names (which must exist and be in no other pool) whether the
	// address is new or already exists. "" or the address itself takes the
	// address out of any pool. When omitted quota_pool is left as it is.
	QuotaPool *string `json:"quota_pool"`
}

// CreateAddressResp is the result of POST /fmsgid.
type CreateAddressResp struct {
	// Created is true when the address didn't exist before.
	Created bool `json:"created"`
	// Reinstated is true when the address already existed, wasn't accepting
	// new messages and now is, because the request set accepting_new true.
	Reinstated bool `json:"reinstated"`
}

// quotaPoolError is a quota_pool the address can't join; its text is returned
// to the caller.
type quotaPoolError string

func (e quotaPoolError) Error() string { return string(e) }

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
	// QuotaPool is the address of the pool owner whose limits and combined
	// usage are reported, omitted when the address is in no pool.
	QuotaPool *string `json:"quotaPool,omitempty"`
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
	var poolOwner string

	// the limits are the quota pool's: the pool owner's when the address is in
	// a pool, else its own
	err = rows.Scan(&ad.Address, &ad.DisplayName, &ad.AcceptingNew, &ad.LimitRecvSizeTotal,
		&ad.LimitRecvSizePerMsg, &ad.LimitRecvSizePer1d, &ad.LimitRecvCountPer1d,
		&ad.LimitSendSizeTotal, &ad.LimitSendSizePerMsg, &ad.LimitSendSizePer1d,
		&ad.LimitSendCountPer1d, &poolOwner, &ad.QuotaPool)
	if err != nil {
		c.AbortWithError(500, err)
		return
	}
	rows.Close()

	// select actuals summed over the quota pool; an aggregate always returns
	// one row, zeros when the pool hasn't sent or received anything
	err = pool.QueryRow(ctx, sqlActuals, poolOwner).Scan(
		&ad.RecvSizeTotal, &ad.RecvSizePer1d, &ad.RecvCountPer1d,
		&ad.SendSizeTotal, &ad.SendSizePer1d, &ad.SendCountPer1d)
	if err != nil {
		c.AbortWithError(500, err)
		return
	}

	c.JSON(http.StatusOK, ad)
}

// postCreateAddress registers an address, idempotently. A new address takes
// the address table's column defaults except for any accepting_new and
// quota_pool given. An existing address is left as it is except for any
// accepting_new and quota_pool given; display_name and the limits are never
// changed — use CSV sync for those.
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
	if req.QuotaPool != nil && *req.QuotaPool != "" && !isValidFmsgAddr(*req.QuotaPool) {
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	resp, err := registerAddress(ctx, req)
	var qpErr quotaPoolError
	if errors.As(err, &qpErr) {
		c.AbortWithStatusJSON(http.StatusUnprocessableEntity, gin.H{"error": qpErr.Error()})
		return
	}
	if err != nil {
		c.AbortWithError(500, err)
		return
	}

	if resp.Created {
		c.JSON(http.StatusCreated, resp)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// registerAddress inserts or updates the requested address in one transaction.
func registerAddress(ctx context.Context, req CreateAddressReq) (CreateAddressResp, error) {
	var resp CreateAddressResp
	addrLower := cases.Fold().String(req.Address)

	// quotaPool is the case folded pool owner to set when req.QuotaPool is
	// given, nil to take the address out of any pool
	var quotaPool *string
	if req.QuotaPool != nil && *req.QuotaPool != "" {
		if p := cases.Fold().String(*req.QuotaPool); p != addrLower {
			quotaPool = &p
		}
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return resp, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // a no-op after commit

	// pools are one level deep: the pool owner must be in no other pool, and
	// an address that owns a pool can't join another
	if quotaPool != nil {
		var ownerPool *string
		err := tx.QueryRow(ctx, sqlSelectQuotaPoolForShare, *quotaPool).Scan(&ownerPool)
		if errors.Is(err, pgx.ErrNoRows) {
			return resp, quotaPoolError("quota_pool address not found")
		}
		if err != nil {
			return resp, err
		}
		if ownerPool != nil && *ownerPool != *quotaPool {
			return resp, quotaPoolError("quota_pool address is in another quota pool")
		}
		var isOwner bool
		if err := tx.QueryRow(ctx, sqlIsPoolOwner, addrLower).Scan(&isOwner); err != nil {
			return resp, err
		}
		if isOwner {
			return resp, quotaPoolError("address owns a quota pool")
		}
	}

	// only the columns given are inserted so the rest take their defaults
	cols := []string{"address_lower", "address", "display_name"}
	args := []any{addrLower, req.Address, req.DisplayName}
	if req.AcceptingNew != nil {
		cols = append(cols, "accepting_new")
		args = append(args, *req.AcceptingNew)
	}
	if req.QuotaPool != nil {
		cols = append(cols, "quota_pool")
		args = append(args, quotaPool)
	}
	tag, err := tx.Exec(ctx, insertAddressSQL(cols), args...)
	if err != nil {
		return resp, err
	}
	resp.Created = tag.RowsAffected() == 1

	if !resp.Created && (req.AcceptingNew != nil || req.QuotaPool != nil) {
		var wasAccepting bool
		if err := tx.QueryRow(ctx, sqlSelectAcceptingNewForUpdate, addrLower).Scan(&wasAccepting); err != nil {
			return resp, err
		}
		var set []string
		args := []any{addrLower}
		if req.AcceptingNew != nil {
			args = append(args, *req.AcceptingNew)
			set = append(set, fmt.Sprintf("accepting_new = $%d", len(args)))
		}
		if req.QuotaPool != nil {
			args = append(args, quotaPool)
			set = append(set, fmt.Sprintf("quota_pool = $%d", len(args)))
		}
		if _, err := tx.Exec(ctx, "update address set "+strings.Join(set, ", ")+" where address_lower = $1;", args...); err != nil {
			return resp, err
		}
		resp.Reinstated = !wasAccepting && req.AcceptingNew != nil && *req.AcceptingNew
	}

	return resp, tx.Commit(ctx)
}

// insertAddressSQL returns an insert of the given address columns, in order as
// $1..$n, that does nothing when the address already exists.
func insertAddressSQL(cols []string) string {
	params := make([]string, len(cols))
	for i := range cols {
		params[i] = fmt.Sprintf("$%d", i+1)
	}
	return "insert into address (" + strings.Join(cols, ", ") + ") values (" +
		strings.Join(params, ", ") + ") on conflict (address_lower) do nothing;"
}

func postAddressTxSend(c *gin.Context) {
	postAddressTx(c, TypeSend)
}

func postAddressTxRecv(c *gin.Context) {
	postAddressTx(c, TypeRecv)
}

func postAddressTxSendRelease(c *gin.Context) {
	postAddressTx(c, TypeSendRelease)
}

func postAddressTxRecvRelease(c *gin.Context) {
	postAddressTx(c, TypeRecvRelease)
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

	if !isValidFmsgAddr(tx.Address) {
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	// address_lower references address rows, which are keyed by the case
	// folded address
	addrLower := cases.Fold().String(tx.Address)

	_, err = pool.Exec(ctx, sqlInsertTx, addrLower, tx.Timestamp, typ, tx.Size)
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
	r.POST("/fmsgid/send/release", postAddressTxSendRelease)
	r.POST("/fmsgid/recv/release", postAddressTxRecvRelease)
	err = r.Run(":" + port)
	if err != nil {
		log.Fatalf("ERROR: Running gin engine: %s", err)
	}
}
