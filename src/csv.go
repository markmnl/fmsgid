package main

import (
	"context"
	"encoding/csv"
	"fmt"
	"log"
	"os"
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

// parseCSV reads a CSV file and returns address rows. The CSV must have a header
// row with column names matching the address table columns. Only the "address"
// column is required; others use defaults (accepting_new=true, limits=-1).
// Malformed rows are logged and skipped.
func parseCSV(filePath string) ([]addressRow, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("opening CSV file: %w", err)
	}
	defer f.Close()

	reader := csv.NewReader(f)
	reader.TrimLeadingSpace = true

	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("reading CSV: %w", err)
	}

	if len(records) < 1 {
		return nil, fmt.Errorf("CSV file is empty (no header row)")
	}

	// Build column index from header
	header := records[0]
	colIdx := make(map[string]int, len(header))
	for i, name := range header {
		colIdx[strings.TrimSpace(strings.ToLower(name))] = i
	}

	addrCol, ok := colIdx["address"]
	if !ok {
		return nil, fmt.Errorf("CSV missing required 'address' column in header")
	}

	fold := cases.Fold()
	var rows []addressRow

	for lineNum, record := range records[1:] {
		csvLine := lineNum + 2 // 1-indexed, skip header

		if len(record) <= addrCol {
			log.Printf("WARN: CSV line %d: not enough columns, skipping", csvLine)
			continue
		}

		addr := strings.TrimSpace(record[addrCol])
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

		if idx, ok := colIdx["display_name"]; ok && idx < len(record) {
			row.DisplayName = strings.TrimSpace(record[idx])
		}
		if idx, ok := colIdx["accepting_new"]; ok && idx < len(record) {
			val := strings.TrimSpace(strings.ToLower(record[idx]))
			if val != "" {
				b, err := strconv.ParseBool(val)
				if err != nil {
					log.Printf("WARN: CSV line %d: invalid accepting_new %q, skipping row", csvLine, val)
					continue
				}
				row.AcceptingNew = b
			}
		}

		parseOK := true
		parseInt64Col := func(colName string, dest *int64) {
			if idx, ok := colIdx[colName]; ok && idx < len(record) {
				val := strings.TrimSpace(record[idx])
				if val != "" {
					n, err := strconv.ParseInt(val, 10, 64)
					if err != nil {
						log.Printf("WARN: CSV line %d: invalid %s %q, skipping row", csvLine, colName, val)
						parseOK = false
						return
					}
					*dest = n
				}
			}
		}

		parseInt64Col("limit_recv_size_total", &row.LimitRecvSizeTotal)
		parseInt64Col("limit_recv_size_per_msg", &row.LimitRecvSizePerMsg)
		parseInt64Col("limit_recv_size_per_1d", &row.LimitRecvSizePer1d)
		parseInt64Col("limit_recv_count_per_1d", &row.LimitRecvCountPer1d)
		parseInt64Col("limit_send_size_total", &row.LimitSendSizeTotal)
		parseInt64Col("limit_send_size_per_msg", &row.LimitSendSizePerMsg)
		parseInt64Col("limit_send_size_per_1d", &row.LimitSendSizePer1d)
		parseInt64Col("limit_send_count_per_1d", &row.LimitSendCountPer1d)

		if !parseOK {
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
			br.Close()
			return fmt.Errorf("upserting address: %w", err)
		}
	}
	br.Close()

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
// re-syncing on each write. It blocks until the context is cancelled.
func startCSVWatcher(ctx context.Context, pool *pgxpool.Pool, filePath string) {
	// Initial sync
	doSync := func() {
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

	if err := watcher.Add(filePath); err != nil {
		log.Printf("ERROR: Watching CSV file %s: %s", filePath, err)
		return
	}

	log.Printf("INFO: Watching CSV file %s for changes", filePath)

	var debounce *time.Timer

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if event.Op&(fsnotify.Write|fsnotify.Create) != 0 {
				// Debounce: wait 500ms after last event before syncing
				if debounce != nil {
					debounce.Stop()
				}
				debounce = time.AfterFunc(500*time.Millisecond, doSync)
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Printf("ERROR: CSV file watcher: %s", err)
		}
	}
}
