package testdb

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestStoreHarnessContract(t *testing.T) {
	backendEnv := strings.ToLower(strings.TrimSpace(os.Getenv("WACALLS_TEST_BACKEND")))
	mariadbDSN := strings.TrimSpace(os.Getenv("WACALLS_TEST_MARIADB_DSN"))

	var backends []Backend

	switch backendEnv {
	case "sqlite":
		backends = []Backend{BackendSQLite}
	case "mariadb":
		if mariadbDSN == "" {
			t.Fatal("WACALLS_TEST_BACKEND is set to mariadb, but WACALLS_TEST_MARIADB_DSN is empty")
		}
		backends = []Backend{BackendMariaDB}
	case "all":
		if mariadbDSN == "" {
			t.Fatal("WACALLS_TEST_BACKEND is set to all, but WACALLS_TEST_MARIADB_DSN is empty")
		}
		backends = []Backend{BackendSQLite, BackendMariaDB}
	default:
		// Default local development / `go test ./...` behavior:
		backends = append(backends, BackendSQLite)
		if mariadbDSN != "" {
			backends = append(backends, BackendMariaDB)
		} else {
			t.Log("Note: MariaDB store contract skipped because WACALLS_TEST_MARIADB_DSN is not set")
		}
	}

	for _, backend := range backends {
		b := backend
		t.Run(string(b), func(t *testing.T) {
			if os.Getenv("WACALLS_TEST_SIMULATE_FAILURE") == "true" && b == BackendMariaDB {
				t.Fatal("simulated failure during MariaDB test to verify harness trap/cleanup")
			}
			tdb := OpenTestDB(t, b)
			runContractSuite(t, tdb)
		})
	}
}

func runContractSuite(t *testing.T, tdb *TestDB) {
	ctx := context.Background()

	// 1. Schema idempotence: apply DDL twice without error
	t.Run("SchemaIdempotence", func(t *testing.T) {
		ddl := GetSmokeContractDDL(tdb.Backend)
		if _, err := tdb.DB.ExecContext(ctx, ddl); err != nil {
			t.Fatalf("first DDL execution failed: %v", err)
		}
		if _, err := tdb.DB.ExecContext(ctx, ddl); err != nil {
			t.Fatalf("second DDL execution (idempotency check) failed: %v", err)
		}
	})

	// 2. Transaction commit visible
	t.Run("TransactionCommit", func(t *testing.T) {
		tx, err := tdb.DB.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("failed to begin transaction: %v", err)
		}

		now := time.Now().Unix()
		query := `INSERT INTO harness_contract_items (id, tenant_id, item_key, state, version, created_at, updated_at)
			VALUES (?, ?, ?, ?, 1, ?, ?)`
		if _, err := tx.ExecContext(ctx, query, "tx_item_1", "tenant_tx", "key_committed", "new", now, now); err != nil {
			_ = tx.Rollback()
			t.Fatalf("failed to insert in transaction: %v", err)
		}

		if err := tx.Commit(); err != nil {
			t.Fatalf("failed to commit transaction: %v", err)
		}

		var state string
		err = tdb.DB.QueryRowContext(ctx, "SELECT state FROM harness_contract_items WHERE id=?", "tx_item_1").Scan(&state)
		if err != nil {
			t.Fatalf("committed item not found in DB: %v", err)
		}
		if state != "new" {
			t.Fatalf("expected state 'new', got %q", state)
		}
	})

	// 3 & 4. Composite unique constraint and duplicate conflict recognition
	t.Run("UniqueConstraintAndConflict", func(t *testing.T) {
		now := time.Now().Unix()
		query := `INSERT INTO harness_contract_items (id, tenant_id, item_key, state, version, created_at, updated_at)
			VALUES (?, ?, ?, ?, 1, ?, ?)`

		// First insert must succeed
		_, err := tdb.DB.ExecContext(ctx, query, "uniq_item_1", "tenant_uq", "shared_key", "active", now, now)
		if err != nil {
			t.Fatalf("first unique insert failed: %v", err)
		}

		// Second insert with different ID but same (tenant_id, item_key) must fail with unique violation
		_, err = tdb.DB.ExecContext(ctx, query, "uniq_item_2", "tenant_uq", "shared_key", "active", now, now)
		if err == nil {
			t.Fatal("expected unique constraint violation, got nil error")
		}
		if !IsUniqueViolation(err) {
			t.Fatalf("expected IsUniqueViolation(err) == true, got error: %v", err)
		}
	})

	// 5. CAS with RowsAffected (1 winner, 0 for losers)
	t.Run("CASAndRowsAffected", func(t *testing.T) {
		now := time.Now().Unix()
		query := `INSERT INTO harness_contract_items (id, tenant_id, item_key, state, version, created_at, updated_at)
			VALUES (?, ?, ?, ?, 1, ?, ?)`
		if _, err := tdb.DB.ExecContext(ctx, query, "cas_item_1", "tenant_cas", "cas_key", "pending", now, now); err != nil {
			t.Fatalf("failed to setup CAS item: %v", err)
		}

		casQuery := `UPDATE harness_contract_items SET state=?, version=version+1, updated_at=? WHERE id=? AND state=?`

		// Winner claim
		res, err := tdb.DB.ExecContext(ctx, casQuery, "claimed", now+1, "cas_item_1", "pending")
		if err != nil {
			t.Fatalf("winner CAS failed with error: %v", err)
		}
		rows, err := res.RowsAffected()
		if err != nil {
			t.Fatalf("failed to read RowsAffected: %v", err)
		}
		if rows != 1 {
			t.Fatalf("expected winner RowsAffected() == 1, got %d", rows)
		}

		// Loser attempt with old state "pending"
		resLoser, err := tdb.DB.ExecContext(ctx, casQuery, "claimed_again", now+2, "cas_item_1", "pending")
		if err != nil {
			t.Fatalf("loser CAS failed with error: %v", err)
		}
		loserRows, err := resLoser.RowsAffected()
		if err != nil {
			t.Fatalf("failed to read loser RowsAffected: %v", err)
		}
		if loserRows != 0 {
			t.Fatalf("expected loser RowsAffected() == 0, got %d", loserRows)
		}
	})

	// 6. Rollback integral
	t.Run("TransactionRollback", func(t *testing.T) {
		tx, err := tdb.DB.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("failed to begin tx: %v", err)
		}

		now := time.Now().Unix()
		query := `INSERT INTO harness_contract_items (id, tenant_id, item_key, state, version, created_at, updated_at)
			VALUES (?, ?, ?, ?, 1, ?, ?)`
		if _, err := tx.ExecContext(ctx, query, "rollback_item_1", "tenant_rb", "rb_key", "to_be_rolled_back", now, now); err != nil {
			t.Fatalf("failed to insert inside tx: %v", err)
		}

		if err := tx.Rollback(); err != nil {
			t.Fatalf("failed to rollback tx: %v", err)
		}

		var id string
		err = tdb.DB.QueryRowContext(ctx, "SELECT id FROM harness_contract_items WHERE id=?", "rollback_item_1").Scan(&id)
		if err != sql.ErrNoRows {
			t.Fatalf("expected sql.ErrNoRows for rolled-back item, got error: %v", err)
		}
	})

	// 7 & 8. Concurrency across multiple separate connections/pools
	t.Run("ConcurrentCASMultipleConnections", func(t *testing.T) {
		now := time.Now().Unix()
		itemID := "race_item_1"

		insertQuery := `INSERT INTO harness_contract_items (id, tenant_id, item_key, state, version, created_at, updated_at)
			VALUES (?, ?, ?, ?, 1, ?, ?)`
		if _, err := tdb.DB.ExecContext(ctx, insertQuery, itemID, "tenant_race", "race_key", "open", now, now); err != nil {
			t.Fatalf("failed to insert race item: %v", err)
		}

		const numWorkers = 8
		pools := make([]*sql.DB, numWorkers)
		pools[0] = tdb.DB
		for i := 1; i < numWorkers; i++ {
			pools[i] = tdb.OpenSecondary(t)
		}

		startGate := make(chan struct{})
		var wg sync.WaitGroup
		var winners int32
		var losers int32
		var winningWorker string
		var winMu sync.Mutex

		for i := 0; i < numWorkers; i++ {
			workerID := fmt.Sprintf("worker-%d", i)
			dbPool := pools[i]
			wg.Add(1)

			go func(id string, pool *sql.DB) {
				defer wg.Done()
				<-startGate // Synchronize start

				cas := `UPDATE harness_contract_items
					SET state='claimed', winner_id=?, version=version+1, updated_at=?
					WHERE id=? AND state='open'`
				res, err := pool.ExecContext(context.Background(), cas, id, time.Now().Unix(), itemID)
				if err != nil {
					t.Errorf("worker %s error during CAS: %v", id, err)
					return
				}

				ra, err := res.RowsAffected()
				if err != nil {
					t.Errorf("worker %s error reading RowsAffected: %v", id, err)
					return
				}

				if ra == 1 {
					atomic.AddInt32(&winners, 1)
					winMu.Lock()
					winningWorker = id
					winMu.Unlock()
				} else {
					atomic.AddInt32(&losers, 1)
				}
			}(workerID, dbPool)
		}

		close(startGate) // Release all workers simultaneously
		wg.Wait()

		if winners != 1 {
			t.Fatalf("expected exactly 1 CAS winner, got %d winners (losers: %d)", winners, losers)
		}
		if losers != numWorkers-1 {
			t.Fatalf("expected %d CAS losers, got %d", numWorkers-1, losers)
		}

		var recordedWinner string
		var finalState string
		err := tdb.DB.QueryRowContext(ctx, "SELECT winner_id, state FROM harness_contract_items WHERE id=?", itemID).Scan(&recordedWinner, &finalState)
		if err != nil {
			t.Fatalf("failed to query race item result: %v", err)
		}
		if finalState != "claimed" {
			t.Fatalf("expected final state 'claimed', got %q", finalState)
		}
		if recordedWinner != winningWorker {
			t.Fatalf("expected recorded winner %q, got %q", winningWorker, recordedWinner)
		}
	})
}
