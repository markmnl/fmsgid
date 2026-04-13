package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTestCSV(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.csv")
	err := os.WriteFile(path, []byte(content), 0600)
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseCSV_AllColumns(t *testing.T) {
	csv := `address,display_name,accepting_new,limit_recv_size_total,limit_recv_size_per_msg,limit_recv_size_per_1d,limit_recv_count_per_1d,limit_send_size_total,limit_send_size_per_msg,limit_send_size_per_1d,limit_send_count_per_1d
@alice@example.com,Alice,true,100,200,300,400,500,600,700,800
`
	rows, err := parseCSV(writeTestCSV(t, csv))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	r := rows[0]
	if r.Address != "@alice@example.com" {
		t.Errorf("Address = %q", r.Address)
	}
	if r.DisplayName != "Alice" {
		t.Errorf("DisplayName = %q", r.DisplayName)
	}
	if r.AcceptingNew != true {
		t.Errorf("AcceptingNew = %v", r.AcceptingNew)
	}
	if r.LimitRecvSizeTotal != 100 {
		t.Errorf("LimitRecvSizeTotal = %d", r.LimitRecvSizeTotal)
	}
	if r.LimitRecvSizePerMsg != 200 {
		t.Errorf("LimitRecvSizePerMsg = %d", r.LimitRecvSizePerMsg)
	}
	if r.LimitRecvSizePer1d != 300 {
		t.Errorf("LimitRecvSizePer1d = %d", r.LimitRecvSizePer1d)
	}
	if r.LimitRecvCountPer1d != 400 {
		t.Errorf("LimitRecvCountPer1d = %d", r.LimitRecvCountPer1d)
	}
	if r.LimitSendSizeTotal != 500 {
		t.Errorf("LimitSendSizeTotal = %d", r.LimitSendSizeTotal)
	}
	if r.LimitSendSizePerMsg != 600 {
		t.Errorf("LimitSendSizePerMsg = %d", r.LimitSendSizePerMsg)
	}
	if r.LimitSendSizePer1d != 700 {
		t.Errorf("LimitSendSizePer1d = %d", r.LimitSendSizePer1d)
	}
	if r.LimitSendCountPer1d != 800 {
		t.Errorf("LimitSendCountPer1d = %d", r.LimitSendCountPer1d)
	}
}

func TestParseCSV_AddressOnly(t *testing.T) {
	csv := "address\n@bob@example.com\n"
	rows, err := parseCSV(writeTestCSV(t, csv))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	r := rows[0]
	if r.Address != "@bob@example.com" {
		t.Errorf("Address = %q", r.Address)
	}
	if r.AcceptingNew != true {
		t.Errorf("AcceptingNew should default to true, got %v", r.AcceptingNew)
	}
	if r.LimitRecvSizeTotal != -1 {
		t.Errorf("LimitRecvSizeTotal should default to -1, got %d", r.LimitRecvSizeTotal)
	}
	if r.LimitSendSizePerMsg != -1 {
		t.Errorf("LimitSendSizePerMsg should default to -1, got %d", r.LimitSendSizePerMsg)
	}
}

func TestParseCSV_CaseFolding(t *testing.T) {
	csv := "address\n@Alice@Example.COM\n"
	rows, err := parseCSV(writeTestCSV(t, csv))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].Address != "@Alice@Example.COM" {
		t.Errorf("Address should preserve original case, got %q", rows[0].Address)
	}
	if rows[0].AddressLower != "@alice@example.com" {
		t.Errorf("AddressLower should be case-folded, got %q", rows[0].AddressLower)
	}
}

func TestParseCSV_Defaults(t *testing.T) {
	csv := "address,display_name\n@user@example.com,\n"
	rows, err := parseCSV(writeTestCSV(t, csv))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	r := rows[0]
	if r.DisplayName != "" {
		t.Errorf("DisplayName should be empty, got %q", r.DisplayName)
	}
	if r.AcceptingNew != true {
		t.Errorf("AcceptingNew should default to true")
	}
	limits := []int64{
		r.LimitRecvSizeTotal, r.LimitRecvSizePerMsg, r.LimitRecvSizePer1d, r.LimitRecvCountPer1d,
		r.LimitSendSizeTotal, r.LimitSendSizePerMsg, r.LimitSendSizePer1d, r.LimitSendCountPer1d,
	}
	for i, v := range limits {
		if v != -1 {
			t.Errorf("limit[%d] should default to -1, got %d", i, v)
		}
	}
}

func TestParseCSV_SkipsMalformedRows(t *testing.T) {
	csv := `address,accepting_new,limit_recv_size_total
@good@example.com,true,100
@bad-bool@example.com,notabool,100
@bad-limit@example.com,true,notanumber
@also-good@example.com,false,200
`
	rows, err := parseCSV(writeTestCSV(t, csv))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 valid rows, got %d", len(rows))
	}
	if rows[0].Address != "@good@example.com" {
		t.Errorf("first row Address = %q", rows[0].Address)
	}
	if rows[1].Address != "@also-good@example.com" {
		t.Errorf("second row Address = %q", rows[1].Address)
	}
	if rows[1].AcceptingNew != false {
		t.Errorf("second row AcceptingNew should be false")
	}
}

func TestParseCSV_SkipsEmptyAddress(t *testing.T) {
	csv := "address,display_name\n,Alice\n@real@example.com,Bob\n"
	rows, err := parseCSV(writeTestCSV(t, csv))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].Address != "@real@example.com" {
		t.Errorf("Address = %q", rows[0].Address)
	}
}

func TestParseCSV_EmptyFile(t *testing.T) {
	path := writeTestCSV(t, "")
	_, err := parseCSV(path)
	if err == nil {
		t.Fatal("expected error for empty file")
	}
}

func TestParseCSV_MissingAddressColumn(t *testing.T) {
	csv := "display_name,accepting_new\nAlice,true\n"
	_, err := parseCSV(writeTestCSV(t, csv))
	if err == nil {
		t.Fatal("expected error for missing address column")
	}
}

func TestParseCSV_FileNotFound(t *testing.T) {
	_, err := parseCSV("/nonexistent/path/file.csv")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestParseCSV_ColumnOrderIndependent(t *testing.T) {
	csv := `display_name,limit_send_size_per_msg,address,accepting_new
Carol,4096,@carol@example.com,false
`
	rows, err := parseCSV(writeTestCSV(t, csv))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	r := rows[0]
	if r.Address != "@carol@example.com" {
		t.Errorf("Address = %q", r.Address)
	}
	if r.DisplayName != "Carol" {
		t.Errorf("DisplayName = %q", r.DisplayName)
	}
	if r.AcceptingNew != false {
		t.Errorf("AcceptingNew = %v", r.AcceptingNew)
	}
	if r.LimitSendSizePerMsg != 4096 {
		t.Errorf("LimitSendSizePerMsg = %d", r.LimitSendSizePerMsg)
	}
	// Unspecified limits should still be -1
	if r.LimitRecvSizeTotal != -1 {
		t.Errorf("LimitRecvSizeTotal should be -1, got %d", r.LimitRecvSizeTotal)
	}
}

func TestParseCSV_MultipleRows(t *testing.T) {
	csv := `address,display_name
@a@example.com,Alpha
@b@example.com,Bravo
@c@example.com,Charlie
`
	rows, err := parseCSV(writeTestCSV(t, csv))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(rows))
	}
}

func TestParseCSV_HeaderCaseInsensitive(t *testing.T) {
	csv := "ADDRESS,Display_Name,ACCEPTING_NEW\n@user@example.com,Test,false\n"
	rows, err := parseCSV(writeTestCSV(t, csv))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].DisplayName != "Test" {
		t.Errorf("DisplayName = %q", rows[0].DisplayName)
	}
	if rows[0].AcceptingNew != false {
		t.Errorf("AcceptingNew should be false")
	}
}
