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

// poolDomain is used only by these tests; its rows are deleted before and
// after each test.
const poolDomain = "pooltest.example"

func deletePoolRows(t *testing.T, p *pgxpool.Pool) {
	t.Helper()
	mustExec(t, p, `delete from address_tx where address_lower like '%@`+poolDomain+`'`)
	mustExec(t, p, `update address set quota_pool = null where address_lower like '%@`+poolDomain+`'`)
	mustExec(t, p, `delete from address where address_lower like '%@`+poolDomain+`'`)
}

func poolTestDB(t *testing.T) {
	t.Helper()
	testDB(t)
	deletePoolRows(t, pool)
	t.Cleanup(func() { deletePoolRows(t, pool) })
}

// postAddress POSTs body to /fmsgid and checks the status and response.
func postAddress(t *testing.T, body string, wantCode int, want CreateAddressResp) {
	t.Helper()
	w := serveRoute("POST", "/fmsgid", body)
	if w.Code != wantCode {
		t.Fatalf("POST /fmsgid %s: status %d, want %d (%s)", body, w.Code, wantCode, w.Body.String())
	}
	if wantCode != http.StatusOK && wantCode != http.StatusCreated {
		return
	}
	var got CreateAddressResp
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("POST /fmsgid %s: %s: %q", body, err, w.Body.String())
	}
	if got != want {
		t.Fatalf("POST /fmsgid %s: response %+v, want %+v", body, got, want)
	}
}

func getDetail(t *testing.T, addr string) AddressDetail {
	t.Helper()
	w := serveRoute("GET", "/fmsgid/"+addr, "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s: status %d, want 200", addr, w.Code)
	}
	var ad AddressDetail
	if err := json.Unmarshal(w.Body.Bytes(), &ad); err != nil {
		t.Fatal(err)
	}
	return ad
}

// setLimits gives addr every limit as base plus its position, so the limits of
// two addresses never coincide.
func setLimits(t *testing.T, addr string, base int64) {
	t.Helper()
	mustExec(t, pool, `update address set
		limit_recv_size_total = $2::bigint, limit_recv_size_per_msg = $2::bigint + 1,
		limit_recv_size_per_1d = $2::bigint + 2, limit_recv_count_per_1d = $2::bigint + 3,
		limit_send_size_total = $2::bigint + 4, limit_send_size_per_msg = $2::bigint + 5,
		limit_send_size_per_1d = $2::bigint + 6, limit_send_count_per_1d = $2::bigint + 7
		where address_lower = $1`, addr, base)
}

func checkLimits(t *testing.T, ad AddressDetail, base int64) {
	t.Helper()
	got := []int64{ad.LimitRecvSizeTotal, ad.LimitRecvSizePerMsg, ad.LimitRecvSizePer1d, ad.LimitRecvCountPer1d,
		ad.LimitSendSizeTotal, ad.LimitSendSizePerMsg, ad.LimitSendSizePer1d, ad.LimitSendCountPer1d}
	for i, v := range got {
		if v != base+int64(i) {
			t.Errorf("%s: limits %v, want %d..%d", ad.Address, got, base, base+7)
			return
		}
	}
}

// usage is the six actuals of an AddressDetail, in field order.
type usage [6]int64

func usageOf(ad AddressDetail) usage {
	return usage{ad.RecvSizeTotal, ad.RecvSizePer1d, ad.RecvCountPer1d,
		ad.SendSizeTotal, ad.SendSizePer1d, ad.SendCountPer1d}
}

func recordTx(t *testing.T, route, addr string, ts float64, size int) {
	t.Helper()
	body := fmt.Sprintf(`{"address":%q,"ts":%f,"size":%d}`, addr, ts, size)
	if w := serveRoute("POST", route, body); w.Code != http.StatusOK {
		t.Fatalf("POST %s %s: status %d, want 200", route, body, w.Code)
	}
}

func quotaPoolOf(t *testing.T, addr string) *string {
	t.Helper()
	var qp *string
	if err := pool.QueryRow(context.Background(),
		`select quota_pool from address where address_lower = $1`, addr).Scan(&qp); err != nil {
		t.Fatal(err)
	}
	return qp
}

func acceptingNewOf(t *testing.T, addr string) bool {
	t.Helper()
	var a bool
	if err := pool.QueryRow(context.Background(),
		`select accepting_new from address where address_lower = $1`, addr).Scan(&a); err != nil {
		t.Fatal(err)
	}
	return a
}

// TestAddressDetail_QuotaPool checks a pooled address reports its pool
// owner's limits and the pool's combined usage, the owner reports the same
// usage, and an address in no pool is unaffected.
func TestAddressDetail_QuotaPool(t *testing.T) {
	poolTestDB(t)

	postAddress(t, `{"address":"@Alice@PoolTest.example"}`, http.StatusCreated, CreateAddressResp{Created: true})
	postAddress(t, `{"address":"@alice_bot@pooltest.example","quota_pool":"@ALICE@pooltest.example"}`,
		http.StatusCreated, CreateAddressResp{Created: true})
	postAddress(t, `{"address":"@alice_cli@pooltest.example","quota_pool":"@alice@pooltest.example"}`,
		http.StatusCreated, CreateAddressResp{Created: true})
	postAddress(t, `{"address":"@bob@pooltest.example"}`, http.StatusCreated, CreateAddressResp{Created: true})
	setLimits(t, "@alice@pooltest.example", 1000)
	setLimits(t, "@alice_bot@pooltest.example", 2000)
	setLimits(t, "@alice_cli@pooltest.example", 3000)
	setLimits(t, "@bob@pooltest.example", 4000)

	now := float64(time.Now().UnixMicro()) / 1e6
	old := now - 2*24*60*60
	recordTx(t, "/fmsgid/recv", "@alice@pooltest.example", now, 100)
	recordTx(t, "/fmsgid/recv", "@alice@pooltest.example", old, 1000)
	recordTx(t, "/fmsgid/recv", "@alice_bot@pooltest.example", now, 20)
	recordTx(t, "/fmsgid/recv", "@ALICE_CLI@pooltest.example", now, 3)
	recordTx(t, "/fmsgid/send", "@alice_bot@pooltest.example", now, 40)
	recordTx(t, "/fmsgid/send", "@alice_cli@pooltest.example", old, 500)
	recordTx(t, "/fmsgid/recv", "@bob@pooltest.example", now, 7)
	recordTx(t, "/fmsgid/send", "@bob@pooltest.example", now, 9)

	poolUsage := usage{1123, 123, 3, 540, 40, 1}

	for _, addr := range []string{"@alice_bot@pooltest.example", "@Alice_CLI@pooltest.example"} {
		ad := getDetail(t, addr)
		checkLimits(t, ad, 1000)
		if got := usageOf(ad); got != poolUsage {
			t.Errorf("%s: usage %v, want pool usage %v", addr, got, poolUsage)
		}
		if ad.QuotaPool == nil || *ad.QuotaPool != "@Alice@PoolTest.example" {
			t.Errorf("%s: quotaPool %v, want @Alice@PoolTest.example", addr, ad.QuotaPool)
		}
		if !ad.AcceptingNew {
			t.Errorf("%s: not accepting new", addr)
		}
	}

	owner := getDetail(t, "@alice@pooltest.example")
	checkLimits(t, owner, 1000)
	if got := usageOf(owner); got != poolUsage {
		t.Errorf("owner: usage %v, want pool usage %v", got, poolUsage)
	}
	if owner.QuotaPool != nil {
		t.Errorf("owner: quotaPool %q, want omitted", *owner.QuotaPool)
	}

	bob := getDetail(t, "@bob@pooltest.example")
	checkLimits(t, bob, 4000)
	if got, want := usageOf(bob), (usage{7, 7, 1, 9, 9, 1}); got != want {
		t.Errorf("unpooled: usage %v, want %v", got, want)
	}
	if bob.QuotaPool != nil {
		t.Errorf("unpooled: quotaPool %q, want omitted", *bob.QuotaPool)
	}

	// accepting_new stays per address
	mustExec(t, pool, `update address set accepting_new = false where address_lower = '@alice_bot@pooltest.example'`)
	if getDetail(t, "@alice_bot@pooltest.example").AcceptingNew {
		t.Error("pooled address accepting new after disabling it")
	}
	if !getDetail(t, "@alice@pooltest.example").AcceptingNew {
		t.Error("owner not accepting new after disabling a pool member")
	}

	// the response keeps every pre-pool field and adds only quotaPool
	w := serveRoute("GET", "/fmsgid/@bob@pooltest.example", "")
	var fields map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &fields); err != nil {
		t.Fatal(err)
	}
	if _, ok := fields["quotaPool"]; ok {
		t.Error("unpooled response has quotaPool")
	}
}

// TestAddressDetail_QuotaPoolOwnerMissing checks quota_pool can't name an
// address that doesn't exist, and that deleting a pool owner leaves its
// members as their own pools.
func TestAddressDetail_QuotaPoolOwnerMissing(t *testing.T) {
	poolTestDB(t)
	ctx := context.Background()

	postAddress(t, `{"address":"@solo@pooltest.example"}`, http.StatusCreated, CreateAddressResp{Created: true})
	if _, err := pool.Exec(ctx, `update address set quota_pool = '@nobody@pooltest.example'
		where address_lower = '@solo@pooltest.example'`); err == nil {
		t.Error("quota_pool naming a missing address was accepted")
	}
	postAddress(t, `{"address":"@solo@pooltest.example","quota_pool":"@nobody@pooltest.example"}`,
		http.StatusUnprocessableEntity, CreateAddressResp{})
	postAddress(t, `{"address":"@new@pooltest.example","quota_pool":"@nobody@pooltest.example"}`,
		http.StatusUnprocessableEntity, CreateAddressResp{})
	if w := serveRoute("GET", "/fmsgid/@new@pooltest.example", ""); w.Code != http.StatusNotFound {
		t.Errorf("address created despite a missing quota_pool: status %d", w.Code)
	}

	postAddress(t, `{"address":"@gone@pooltest.example"}`, http.StatusCreated, CreateAddressResp{Created: true})
	postAddress(t, `{"address":"@solo@pooltest.example","quota_pool":"@gone@pooltest.example"}`,
		http.StatusOK, CreateAddressResp{})
	setLimits(t, "@gone@pooltest.example", 1000)
	setLimits(t, "@solo@pooltest.example", 2000)
	checkLimits(t, getDetail(t, "@solo@pooltest.example"), 1000)

	mustExec(t, pool, `delete from address where address_lower = '@gone@pooltest.example'`)
	ad := getDetail(t, "@solo@pooltest.example")
	checkLimits(t, ad, 2000)
	if ad.QuotaPool != nil {
		t.Errorf("quotaPool %q after its owner was deleted, want omitted", *ad.QuotaPool)
	}
	if qp := quotaPoolOf(t, "@solo@pooltest.example"); qp != nil {
		t.Errorf("quota_pool %q after its owner was deleted, want NULL", *qp)
	}
}

// TestPostCreateAddress_QuotaPool checks setting, clearing and refusing
// quota_pool through POST /fmsgid.
func TestPostCreateAddress_QuotaPool(t *testing.T) {
	poolTestDB(t)

	postAddress(t, `{"address":"@owner@pooltest.example"}`, http.StatusCreated, CreateAddressResp{Created: true})
	postAddress(t, `{"address":"@other@pooltest.example"}`, http.StatusCreated, CreateAddressResp{Created: true})
	postAddress(t, `{"address":"@member@pooltest.example","quota_pool":"@Owner@pooltest.example"}`,
		http.StatusCreated, CreateAddressResp{Created: true})
	if qp := quotaPoolOf(t, "@member@pooltest.example"); qp == nil || *qp != "@owner@pooltest.example" {
		t.Fatalf("quota_pool %v, want @owner@pooltest.example (case folded)", qp)
	}

	// omitting quota_pool leaves it as it is
	postAddress(t, `{"address":"@member@pooltest.example"}`, http.StatusOK, CreateAddressResp{})
	if quotaPoolOf(t, "@member@pooltest.example") == nil {
		t.Fatal("quota_pool cleared by a request without one")
	}

	// pools are one level deep
	postAddress(t, `{"address":"@nested@pooltest.example","quota_pool":"@member@pooltest.example"}`,
		http.StatusUnprocessableEntity, CreateAddressResp{})
	postAddress(t, `{"address":"@owner@pooltest.example","quota_pool":"@other@pooltest.example"}`,
		http.StatusUnprocessableEntity, CreateAddressResp{})
	if qp := quotaPoolOf(t, "@owner@pooltest.example"); qp != nil {
		t.Fatalf("pool owner joined another pool: %q", *qp)
	}

	postAddress(t, `{"address":"@member@pooltest.example","quota_pool":"alice"}`,
		http.StatusBadRequest, CreateAddressResp{})

	// the address itself, or "", takes it out of any pool
	postAddress(t, `{"address":"@member@pooltest.example","quota_pool":"@MEMBER@pooltest.example"}`,
		http.StatusOK, CreateAddressResp{})
	if qp := quotaPoolOf(t, "@member@pooltest.example"); qp != nil {
		t.Fatalf("quota_pool %q after setting it to the address itself, want NULL", *qp)
	}
	postAddress(t, `{"address":"@member@pooltest.example","quota_pool":"@other@pooltest.example"}`,
		http.StatusOK, CreateAddressResp{})
	postAddress(t, `{"address":"@member@pooltest.example","quota_pool":""}`, http.StatusOK, CreateAddressResp{})
	if qp := quotaPoolOf(t, "@member@pooltest.example"); qp != nil {
		t.Fatalf("quota_pool %q after clearing it, want NULL", *qp)
	}

	// owner and member may now both be pooled
	postAddress(t, `{"address":"@owner@pooltest.example","quota_pool":"@other@pooltest.example"}`,
		http.StatusOK, CreateAddressResp{})
}

// TestPostCreateAddress_Claim checks that a claim registers an address only
// for its pool: new, or already in that pool. An address someone else holds,
// in no pool or another one, is refused and left unchanged.
func TestPostCreateAddress_Claim(t *testing.T) {
	poolTestDB(t)

	postAddress(t, `{"address":"@alice@pooltest.example"}`, http.StatusCreated, CreateAddressResp{Created: true})
	postAddress(t, `{"address":"@carol@pooltest.example"}`, http.StatusCreated, CreateAddressResp{Created: true})

	// a new address is created in the pool
	postAddress(t, `{"address":"@alice_bot@pooltest.example","quota_pool":"@alice@pooltest.example","claim":true}`,
		http.StatusCreated, CreateAddressResp{Created: true})
	if qp := quotaPoolOf(t, "@alice_bot@pooltest.example"); qp == nil || *qp != "@alice@pooltest.example" {
		t.Fatalf("quota_pool %v, want @alice@pooltest.example", qp)
	}

	// one already in the pool is used, and may be reinstated
	postAddress(t, `{"address":"@alice_bot@pooltest.example","accepting_new":false}`, http.StatusOK, CreateAddressResp{})
	postAddress(t, `{"address":"@ALICE_BOT@pooltest.example","accepting_new":true,"quota_pool":"@Alice@pooltest.example","claim":true}`,
		http.StatusOK, CreateAddressResp{Reinstated: true})

	// someone else's address, in no pool: refused and unchanged
	postAddress(t, `{"address":"@carol_bot@pooltest.example","accepting_new":false}`, http.StatusCreated, CreateAddressResp{Created: true})
	postAddress(t, `{"address":"@carol_bot@pooltest.example","accepting_new":true,"quota_pool":"@carol@pooltest.example","claim":true}`,
		http.StatusConflict, CreateAddressResp{})
	if qp := quotaPoolOf(t, "@carol_bot@pooltest.example"); qp != nil {
		t.Fatalf("refused claim set quota_pool %q", *qp)
	}
	if acceptingNewOf(t, "@carol_bot@pooltest.example") {
		t.Fatal("refused claim reinstated the address")
	}

	// an address in another pool: refused and unchanged
	postAddress(t, `{"address":"@alice_bot@pooltest.example","quota_pool":"@carol@pooltest.example","claim":true}`,
		http.StatusConflict, CreateAddressResp{})
	if qp := quotaPoolOf(t, "@alice_bot@pooltest.example"); qp == nil || *qp != "@alice@pooltest.example" {
		t.Fatalf("refused claim moved the address to %v", qp)
	}

	// claiming an address for its own pool (the address itself) needs it to
	// be in no pool
	postAddress(t, `{"address":"@carol@pooltest.example","quota_pool":"@carol@pooltest.example","claim":true}`,
		http.StatusOK, CreateAddressResp{})
	postAddress(t, `{"address":"@alice_bot@pooltest.example","quota_pool":"","claim":true}`,
		http.StatusConflict, CreateAddressResp{})

	// a claim names a pool
	postAddress(t, `{"address":"@dave@pooltest.example","claim":true}`, http.StatusBadRequest, CreateAddressResp{})
}

// TestPostCreateAddress_AcceptingNew checks disabling and reinstating an
// address through POST /fmsgid, and that the response says when an address
// was reinstated.
func TestPostCreateAddress_AcceptingNew(t *testing.T) {
	poolTestDB(t)
	const addr = "@agent@pooltest.example"

	postAddress(t, `{"address":"@Agent@pooltest.example","display_name":"Agent"}`,
		http.StatusCreated, CreateAddressResp{Created: true})
	postAddress(t, `{"address":"@agent@pooltest.example"}`, http.StatusOK, CreateAddressResp{})
	postAddress(t, `{"address":"@agent@pooltest.example","accepting_new":true}`, http.StatusOK, CreateAddressResp{})

	postAddress(t, `{"address":"@AGENT@pooltest.example","accepting_new":false}`, http.StatusOK, CreateAddressResp{})
	if acceptingNewOf(t, addr) {
		t.Fatal("accepting_new still true after setting it false")
	}
	postAddress(t, `{"address":"@agent@pooltest.example","accepting_new":false}`, http.StatusOK, CreateAddressResp{})

	// registering without accepting_new leaves a disabled address disabled
	postAddress(t, `{"address":"@agent@pooltest.example"}`, http.StatusOK, CreateAddressResp{})
	if acceptingNewOf(t, addr) {
		t.Fatal("accepting_new set true by a request without it")
	}

	postAddress(t, `{"address":"@agent@pooltest.example","accepting_new":true}`,
		http.StatusOK, CreateAddressResp{Reinstated: true})
	if !acceptingNewOf(t, addr) {
		t.Fatal("accepting_new still false after reinstating")
	}
	postAddress(t, `{"address":"@agent@pooltest.example","accepting_new":true}`, http.StatusOK, CreateAddressResp{})

	// display_name and the original spelling are never changed
	ad := getDetail(t, addr)
	if ad.Address != "@Agent@pooltest.example" || ad.DisplayName != "Agent" {
		t.Errorf("address %q display name %q changed", ad.Address, ad.DisplayName)
	}

	// a new address may be created not accepting new messages
	postAddress(t, `{"address":"@held@pooltest.example","accepting_new":false}`,
		http.StatusCreated, CreateAddressResp{Created: true})
	if acceptingNewOf(t, "@held@pooltest.example") {
		t.Error("new address accepting new despite accepting_new false")
	}

	// reinstating and pooling in one request
	postAddress(t, `{"address":"@owner@pooltest.example"}`, http.StatusCreated, CreateAddressResp{Created: true})
	postAddress(t, `{"address":"@held@pooltest.example","accepting_new":true,"quota_pool":"@owner@pooltest.example"}`,
		http.StatusOK, CreateAddressResp{Reinstated: true})
	if quotaPoolOf(t, "@held@pooltest.example") == nil || !acceptingNewOf(t, "@held@pooltest.example") {
		t.Error("reinstate with quota_pool didn't apply both")
	}
}

// TestDDL_QuotaPoolIdempotent re-applies dd.sql and checks quota_pool has
// exactly one foreign key and its index.
func TestDDL_QuotaPoolIdempotent(t *testing.T) {
	testDB(t)
	ctx := context.Background()
	ddl, err := os.ReadFile("../dd.sql")
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, pool, string(ddl))

	var fks, idx int
	if err := pool.QueryRow(ctx, `select count(*) from pg_constraint c
		join pg_attribute a on a.attrelid = c.conrelid and a.attnum = any (c.conkey)
		where c.conrelid = 'address'::regclass and c.contype = 'f' and a.attname = 'quota_pool'`).Scan(&fks); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select count(*) from pg_indexes
		where tablename = 'address' and indexname = 'address_quota_pool'`).Scan(&idx); err != nil {
		t.Fatal(err)
	}
	if fks != 1 || idx != 1 {
		t.Errorf("quota_pool foreign keys %d, indexes %d, want 1 and 1", fks, idx)
	}
}
