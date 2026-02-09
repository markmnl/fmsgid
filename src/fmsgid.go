package main

import (
	"context"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/pgxpool"
)

const (
	TypeRecv = 1
	TypeSend = 2
)

type AddressTx struct {
	Address   string  `json:"address"`
	Timestamp float64 `json:"ts"`
	Size      int     `json:"size"`
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

func testDb() error {
	ctx := context.Background()
	db, err := pgx.Connect(ctx, "")
	if err != nil {
		return err
	}
	defer db.Close(ctx)
	err = db.Ping(ctx)
	if err != nil {
		return err
	}
	// TODO check at least tables exist
	return nil
}

func getAddressDetail(c *gin.Context) {
	ctx := c.Request.Context()

	pool, err := pgxpool.Connect(ctx, "")
	if err != nil {
		c.AbortWithError(500, err)
	}
	defer pool.Close()

	// TODO move data to body to hide
	addr, hasAddr := c.Params.Get("address")
	if !hasAddr {
		c.AbortWithStatus(400)
	}

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

	pool, err := pgxpool.Connect(ctx, "")
	if err != nil {
		c.AbortWithError(500, err)
	}
	defer pool.Close()

	_, err = pool.Exec(ctx, sqlInsertTx, tx.Address, tx.Timestamp, typ, tx.Size)
	if err != nil {
		c.AbortWithError(500, err)
		return
	}

}

func main() {
	log.SetPrefix("fmsgid: ")
	err := testDb()
	if err != nil {
		log.Fatalf("ERROR: Failed to initDb: %s", err)
	}
	log.Println("INFO: Database initalized")
	r := gin.Default()
	r.GET("/addr/:address", getAddressDetail)
	r.POST("/addr/send", postAddressTxSend)
	r.POST("/addr/recv", postAddressTxRecv)
	err = r.Run(":8080")
	if err != nil {
		log.Fatalf("ERROR: Running gin engine: %s", err)
	}
}
