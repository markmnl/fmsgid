package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v4/pgxpool"
)

// usageDomain is used only by these tests; its rows are deleted before and
// after each test.
const usageDomain = "usagetest.example"

func deleteUsageRows(t *testing.T, p *pgxpool.Pool) {
	t.Helper()
	mustExec(t, p, `delete from address_tx where address_lower like '%@`+usageDomain+`'`)
	mustExec(t, p, `delete from address where address_lower like '%@`+usageDomain+`'`)
}

// TestAddressDetail_Usage records messages of both types, some older than a
// day, some sharing a timestamp and some under a differently cased address,
// and checks every usage field of the address detail.
func TestAddressDetail_Usage(t *testing.T) {
	testDB(t)
	deleteUsageRows(t, pool)
	t.Cleanup(func() { deleteUsageRows(t, pool) })

	if w := serveRoute("POST", "/fmsgid", `{"address":"@Usage@UsageTest.example"}`); w.Code != http.StatusCreated {
		t.Fatalf("create address: status %d, want 201", w.Code)
	}

	now := float64(time.Now().UnixMicro()) / 1e6
	old := now - 2*24*60*60
	tx := func(route, addr string, ts float64, size int) {
		t.Helper()
		body := fmt.Sprintf(`{"address":%q,"ts":%f,"size":%d}`, addr, ts, size)
		if w := serveRoute("POST", route, body); w.Code != http.StatusOK {
			t.Fatalf("POST %s %s: status %d, want 200", route, body, w.Code)
		}
	}

	tx("/fmsgid/recv", "@usage@usagetest.example", old, 5000)
	tx("/fmsgid/recv", "@usage@usagetest.example", now, 100)
	tx("/fmsgid/recv", "@USAGE@usagetest.example", now, 200) // same timestamp, other case
	tx("/fmsgid/recv", "@Usage@UsageTest.example", now-60, 300)
	tx("/fmsgid/send", "@usage@usagetest.example", old, 1000)
	tx("/fmsgid/send", "@usage@usagetest.example", now, 10)
	tx("/fmsgid/send", "@usage@USAGETEST.EXAMPLE", now, 20) // same timestamp, other case

	w := serveRoute("GET", "/fmsgid/@usage@usagetest.example", "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET: status %d, want 200", w.Code)
	}
	var ad AddressDetail
	if err := json.Unmarshal(w.Body.Bytes(), &ad); err != nil {
		t.Fatal(err)
	}

	got := map[string]int64{
		"RecvSizeTotal":  ad.RecvSizeTotal,
		"RecvSizePer1d":  ad.RecvSizePer1d,
		"RecvCountPer1d": ad.RecvCountPer1d,
		"SendSizeTotal":  ad.SendSizeTotal,
		"SendSizePer1d":  ad.SendSizePer1d,
		"SendCountPer1d": ad.SendCountPer1d,
	}
	want := map[string]int64{
		"RecvSizeTotal":  5600,
		"RecvSizePer1d":  600,
		"RecvCountPer1d": 3,
		"SendSizeTotal":  1030,
		"SendSizePer1d":  30,
		"SendCountPer1d": 2,
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %d, want %d", k, got[k], v)
		}
	}
}

// TestAddressDetail_Release checks a release takes a message's size off the
// size total, never below zero, and leaves the daily usage as it was.
func TestAddressDetail_Release(t *testing.T) {
	testDB(t)
	deleteUsageRows(t, pool)
	t.Cleanup(func() { deleteUsageRows(t, pool) })

	if w := serveRoute("POST", "/fmsgid", `{"address":"@freed@usagetest.example"}`); w.Code != http.StatusCreated {
		t.Fatalf("create address: status %d, want 201", w.Code)
	}
	now := float64(time.Now().UnixMicro()) / 1e6
	tx := func(route string, size int) {
		t.Helper()
		body := fmt.Sprintf(`{"address":"@Freed@usagetest.example","ts":%f,"size":%d}`, now, size)
		if w := serveRoute("POST", route, body); w.Code != http.StatusOK {
			t.Fatalf("POST %s: status %d, want 200", route, w.Code)
		}
	}
	tx("/fmsgid/recv", 500)
	tx("/fmsgid/recv", 300)
	tx("/fmsgid/send", 70)
	tx("/fmsgid/recv/release", 300)
	tx("/fmsgid/send/release", 100) // more than was sent: the total stops at 0

	w := serveRoute("GET", "/fmsgid/@freed@usagetest.example", "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET: status %d, want 200", w.Code)
	}
	var ad AddressDetail
	if err := json.Unmarshal(w.Body.Bytes(), &ad); err != nil {
		t.Fatal(err)
	}
	got := map[string]int64{
		"RecvSizeTotal": ad.RecvSizeTotal, "RecvSizePer1d": ad.RecvSizePer1d, "RecvCountPer1d": ad.RecvCountPer1d,
		"SendSizeTotal": ad.SendSizeTotal, "SendSizePer1d": ad.SendSizePer1d, "SendCountPer1d": ad.SendCountPer1d,
	}
	want := map[string]int64{
		"RecvSizeTotal": 500, "RecvSizePer1d": 800, "RecvCountPer1d": 2,
		"SendSizeTotal": 0, "SendSizePer1d": 70, "SendCountPer1d": 1,
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %d, want %d", k, got[k], v)
		}
	}
}

// TestAddressDetail_NoUsage checks an address that has sent and received
// nothing reports zero usage.
func TestAddressDetail_NoUsage(t *testing.T) {
	testDB(t)
	deleteUsageRows(t, pool)
	t.Cleanup(func() { deleteUsageRows(t, pool) })

	if w := serveRoute("POST", "/fmsgid", `{"address":"@idle@usagetest.example"}`); w.Code != http.StatusCreated {
		t.Fatalf("create address: status %d, want 201", w.Code)
	}
	w := serveRoute("GET", "/fmsgid/@idle@usagetest.example", "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET: status %d, want 200", w.Code)
	}
	var ad AddressDetail
	if err := json.Unmarshal(w.Body.Bytes(), &ad); err != nil {
		t.Fatal(err)
	}
	if ad.RecvSizeTotal != 0 || ad.RecvSizePer1d != 0 || ad.RecvCountPer1d != 0 ||
		ad.SendSizeTotal != 0 || ad.SendSizePer1d != 0 || ad.SendCountPer1d != 0 {
		t.Errorf("usage not zero: %+v", ad)
	}
}

func TestPostAddressTx_InvalidAddress(t *testing.T) {
	for _, route := range []string{"/fmsgid/send", "/fmsgid/recv"} {
		for _, addr := range []string{"", "alice@example.com", "@alice"} {
			body := fmt.Sprintf(`{"address":%q,"ts":1,"size":1}`, addr)
			if w := serveRoute("POST", route, body); w.Code != http.StatusBadRequest {
				t.Errorf("POST %s %s: status %d, want 400", route, body, w.Code)
			}
		}
	}
}

// TestPostCreateAddress_ColumnDefaults checks a new address takes
// accepting_new and every limit from the address table's column defaults,
// using a copy of the table whose defaults differ from dd.sql's.
func TestPostCreateAddress_ColumnDefaults(t *testing.T) {
	testDB(t)
	ctx := context.Background()
	dsn := os.Getenv("FMSGID_TEST_DATABASE_URL")
	const schema = "fmsgid_test_defaults"
	mustExec(t, pool, `drop schema if exists `+schema+` cascade`)
	mustExec(t, pool, `create schema `+schema)
	mustExec(t, pool, `create table `+schema+`.address (like public.address including all)`)
	t.Cleanup(func() {
		p, err := pgxpool.Connect(ctx, dsn)
		if err == nil {
			_, _ = p.Exec(ctx, `drop schema if exists `+schema+` cascade`)
			p.Close()
		}
	})

	defaults := []struct {
		col string
		val int64
	}{
		{"limit_recv_size_total", 11},
		{"limit_recv_size_per_msg", 12},
		{"limit_recv_size_per_1d", 13},
		{"limit_recv_count_per_1d", 14},
		{"limit_send_size_total", 21},
		{"limit_send_size_per_msg", 22},
		{"limit_send_size_per_1d", 23},
		{"limit_send_count_per_1d", 24},
	}
	mustExec(t, pool, `alter table `+schema+`.address alter column accepting_new set default false`)
	for _, d := range defaults {
		mustExec(t, pool, fmt.Sprintf(`alter table %s.address alter column %s set default %d`, schema, d.col, d.val))
	}

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	p, err := pgxpool.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	prev := pool
	pool = p
	defer func() { pool = prev }()

	w := serveRoute("POST", "/fmsgid", `{"address":"@Dflt@Example.com","display_name":"Default"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST: status %d, want 201", w.Code)
	}

	var addr, displayName string
	var acceptingNew bool
	err = pool.QueryRow(ctx, `select address, display_name, accepting_new from address where address_lower = $1`,
		"@dflt@example.com").Scan(&addr, &displayName, &acceptingNew)
	if err != nil {
		t.Fatal(err)
	}
	if addr != "@Dflt@Example.com" || displayName != "Default" || acceptingNew {
		t.Errorf("address %q display_name %q accepting_new %v", addr, displayName, acceptingNew)
	}
	for _, d := range defaults {
		var v int64
		err := pool.QueryRow(ctx, `select `+d.col+` from address where address_lower = $1`,
			"@dflt@example.com").Scan(&v)
		if err != nil {
			t.Fatal(err)
		}
		if v != d.val {
			t.Errorf("%s = %d, want column default %d", d.col, v, d.val)
		}
	}
}
