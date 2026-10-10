package main

import (
	"context"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v4/pgxpool"
)

// Tests that need a PostgreSQL database are skipped unless
// FMSGID_TEST_DATABASE_URL is set to its connection string. They apply
// ../dd.sql and write and delete rows: point them at a throwaway database only.

func init() {
	gin.SetMode(gin.TestMode)
}

// testDB connects the package pool to the test database with dd.sql applied.
func testDB(t *testing.T) {
	t.Helper()
	dsn := os.Getenv("FMSGID_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("FMSGID_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	p, err := pgxpool.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	ddl, err := os.ReadFile("../dd.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Exec(ctx, string(ddl)); err != nil {
		t.Fatalf("applying dd.sql: %s", err)
	}

	prev := pool
	pool = p
	t.Cleanup(func() {
		pool = prev
		p.Close()
	})
}

func mustExec(t *testing.T, p *pgxpool.Pool, sql string, args ...interface{}) {
	t.Helper()
	if _, err := p.Exec(context.Background(), sql, args...); err != nil {
		t.Fatal(err)
	}
}

// serveRoute serves one request through the same routes main registers.
func serveRoute(method, path, body string) *httptest.ResponseRecorder {
	r := gin.New()
	r.GET("/fmsgid/:address", getAddressDetail)
	r.POST("/fmsgid", postCreateAddress)
	r.POST("/fmsgid/send", postAddressTxSend)
	r.POST("/fmsgid/recv", postAddressTxRecv)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
	return w
}
