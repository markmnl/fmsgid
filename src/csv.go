package main

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/pgxpool"
	"golang.org/x/text/cases"
)

type addressRow struct {
	AddressLower        string
	Address             string
	DisplayName         string
	AcceptingNew        bool
	LimitRecvSizeTotal  int64
	LimitRecvSizePerMsg int64
	LimitRecvSizePer1d  int64
	LimitRecvCountPer1d int64
	LimitSendSizeTotal  int64
	LimitSendSizePerMsg int64
	LimitSendSizePer1d  int64
	LimitSendCountPer1d int64
}

type csvColumns struct {
	address             int
	displayName         int
	acceptingNew        int
	limitRecvSizeTotal  int
	limitRecvSizePerMsg int
	limitRecvSizePer1d  int
	limitRecvCountPer1d int
	limitSendSizeTotal  int
	limitSendSizePerMsg int
	limitSendSizePer1d  int
	limitSendCountPer1d int
}

func resolveCSVColumns(header []string) (csvColumns, error) {
	colIdx := make(map[string]int, len(header))
	for i, name := range header {
		colIdx[strings.TrimSpace(strings.ToLower(name))] = i
	}
	col := func(name string) int {
		if idx, ok := colIdx[name]; ok {
			return idx
		}
		return -1
	}
	cols := csvColumns{
		address:             col("address"),
		displayName:         col("display_name"),
		acceptingNew:        col("accepting_new"),
		limitRecvSizeTotal:  col("limit_recv_size_total"),
		limitRecvSizePerMsg: col("limit_recv_size_per_msg"),
		limitRecvSizePer1d:  col("limit_recv_size_per_1d"),
		limitRecvCountPer1d: col("limit_recv_count_per_1d"),
		limitSendSizeTotal:  col("limit_send_size_total"),
		limitSendSizePerMsg: col("limit_send_size_per_msg"),
		limitSendSizePer1d:  col("limit_send_size_per_1d"),
		limitSendCountPer1d: col("limit_send_count_per_1d"),
	}
	if cols.address < 0 {
		return csvColumns{}, fmt.Errorf("CSV missing required 'address' column in header")
	}
	return cols, nil
}

func parseOptionalInt64(record []string, idx int, csvLine int, colName string, dest *int64) bool {
	if idx < 0 || idx >= len(record) {
		return true
	}
	val := strings.TrimSpace(record[idx])
	if val == "" {
		return true
	}
	n, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		log.Printf("WARN: CSV line %d: invalid %s %q, skipping row", csvLine, colName, val)
		return false
	}
	*dest = n
	return true
}

// parseCSV reads a CSV file and returns address rows. The CSV must have a header
// row with column names matching the address table columns. Only the "address"
// column is required; a missing or empty column gets accepting_new=true and the
// same limits as the address table's column defaults in dd.sql. Malformed rows
// are logged and skipped.
func parseCSV(filePath string) ([]addressRow, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("opening CSV file: %w", err)
	}
	defer f.Close()

	reader := csv.NewReader(f)
	reader.TrimLeadingSpace = true
	reader.ReuseRecord = true

	header, err := reader.Read()
	if errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("CSV file is empty (no header row)")
	}
	if err != nil {
		return nil, fmt.Errorf("reading CSV: %w", err)
	}

	cols, err := resolveCSVColumns(header)
	if err != nil {
		return nil, err
	}

	fold := cases.Fold()
	var rows []addressRow

	for csvLine := 2; ; csvLine++ {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading CSV: %w", err)
		}

		if len(record) <= cols.address {
			log.Printf("WARN: CSV line %d: not enough columns, skipping", csvLine)
			continue
		}

		addr := strings.TrimSpace(record[cols.address])
		if addr == "" {
			log.Printf("WARN: CSV line %d: empty address, skipping", csvLine)
			continue
		}

		row := addressRow{
			Address:             addr,
			AddressLower:        fold.String(addr),
			AcceptingNew:        true,
			LimitRecvSizeTotal:  102400000,
			LimitRecvSizePerMsg: 10240,
			LimitRecvSizePer1d:  102400,
			LimitRecvCountPer1d: 1000,
			LimitSendSizeTotal:  102400000,
			LimitSendSizePerMsg: 10240,
			LimitSendSizePer1d:  102400,
			LimitSendCountPer1d: 1000,
		}

		if cols.displayName >= 0 && cols.displayName < len(record) {
			row.DisplayName = strings.TrimSpace(record[cols.displayName])
		}
		if cols.acceptingNew >= 0 && cols.acceptingNew < len(record) {
			val := strings.TrimSpace(strings.ToLower(record[cols.acceptingNew]))
			if val != "" {
				b, err := strconv.ParseBool(val)
				if err != nil {
					log.Printf("WARN: CSV line %d: invalid accepting_new %q, skipping row", csvLine, val)
					continue
				}
				row.AcceptingNew = b
			}
		}

		if !parseOptionalInt64(record, cols.limitRecvSizeTotal, csvLine, "limit_recv_size_total", &row.LimitRecvSizeTotal) ||
			!parseOptionalInt64(record, cols.limitRecvSizePerMsg, csvLine, "limit_recv_size_per_msg", &row.LimitRecvSizePerMsg) ||
			!parseOptionalInt64(record, cols.limitRecvSizePer1d, csvLine, "limit_recv_size_per_1d", &row.LimitRecvSizePer1d) ||
			!parseOptionalInt64(record, cols.limitRecvCountPer1d, csvLine, "limit_recv_count_per_1d", &row.LimitRecvCountPer1d) ||
			!parseOptionalInt64(record, cols.limitSendSizeTotal, csvLine, "limit_send_size_total", &row.LimitSendSizeTotal) ||
			!parseOptionalInt64(record, cols.limitSendSizePerMsg, csvLine, "limit_send_size_per_msg", &row.LimitSendSizePerMsg) ||
			!parseOptionalInt64(record, cols.limitSendSizePer1d, csvLine, "limit_send_size_per_1d", &row.LimitSendSizePer1d) ||
			!parseOptionalInt64(record, cols.limitSendCountPer1d, csvLine, "limit_send_count_per_1d", &row.LimitSendCountPer1d) {
			continue
		}

		rows = append(rows, row)
	}

	return rows, nil
}

// syncCSV upserts the given addresses into the database and sets accepting_new=false
// for any addresses in the DB that are not present in the given slice.
func syncCSV(ctx context.Context, pool *pgxpool.Pool, addresses []addressRow) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	batch := &pgx.Batch{}
	addressLowers := make([]string, 0, len(addresses))

	for _, a := range addresses {
		batch.Queue(sqlUpsertAddress,
			a.AddressLower, a.Address, a.DisplayName, a.AcceptingNew,
			a.LimitRecvSizeTotal, a.LimitRecvSizePerMsg, a.LimitRecvSizePer1d, a.LimitRecvCountPer1d,
			a.LimitSendSizeTotal, a.LimitSendSizePerMsg, a.LimitSendSizePer1d, a.LimitSendCountPer1d,
		)
		addressLowers = append(addressLowers, a.AddressLower)
	}

	br := tx.SendBatch(ctx, batch)
	for range addresses {
		if _, err := br.Exec(); err != nil {
			_ = br.Close()
			return fmt.Errorf("upserting address: %w", err)
		}
	}
	if err := br.Close(); err != nil {
		return fmt.Errorf("closing upsert batch: %w", err)
	}

	// Disable addresses not present in the CSV
	if len(addressLowers) > 0 {
		_, err = tx.Exec(ctx, sqlDisableAbsentAddresses, addressLowers)
		if err != nil {
			return fmt.Errorf("disabling absent addresses: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing transaction: %w", err)
	}

	return nil
}

// startCSVWatcher performs an initial CSV sync and then watches the file for changes,
// re-syncing on each write or replace. It blocks until the context is cancelled.
func startCSVWatcher(ctx context.Context, pool *pgxpool.Pool, filePath string) {
	doSync := func() {
		if ctx.Err() != nil {
			return
		}
		addresses, err := parseCSV(filePath)
		if err != nil {
			log.Printf("ERROR: CSV parse: %s", err)
			return
		}
		if err := syncCSV(ctx, pool, addresses); err != nil {
			log.Printf("ERROR: CSV sync: %s", err)
			return
		}
		log.Printf("INFO: CSV sync complete, %d addresses processed", len(addresses))
	}

	doSync()

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("ERROR: Creating CSV file watcher: %s", err)
		return
	}
	defer watcher.Close()

	// Watch the parent directory so that atomic renames (used by many editors
	// and tools) trigger events even when the original inode is replaced.
	dir := filepath.Dir(filePath)
	base := filepath.Base(filePath)

	if err := watcher.Add(dir); err != nil {
		log.Printf("ERROR: Watching directory %s: %s", dir, err)
		return
	}

	log.Printf("INFO: Watching CSV file %s for changes", filePath)

	var debounce *time.Timer
	var debounceC <-chan time.Time
	defer func() {
		if debounce != nil {
			debounce.Stop()
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-debounceC:
			debounce = nil
			debounceC = nil
			doSync()
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			// Filter to events on our target file only.
			if filepath.Base(event.Name) != base {
				continue
			}
			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename|fsnotify.Remove) != 0 {
				// Debounce: wait 500ms after last event before syncing.
				if debounce == nil {
					debounce = time.NewTimer(500 * time.Millisecond)
				} else {
					if !debounce.Stop() {
						select {
						case <-debounce.C:
						default:
						}
					}
					debounce.Reset(500 * time.Millisecond)
				}
				debounceC = debounce.C
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Printf("ERROR: CSV file watcher: %s", err)
		}
	}
}
