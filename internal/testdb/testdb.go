// Package testdb provides an isolated test harness for verifying store contracts
// against SQLite and MariaDB. It is strictly for testing and must not be imported
// by runtime code.
package testdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	_ "modernc.org/sqlite"
)

// Backend specifies which database engine is under test.
type Backend string

const (
	BackendSQLite  Backend = "sqlite"
	BackendMariaDB Backend = "mariadb"
)

// TestDB holds the database handle and metadata for an active test database.
type TestDB struct {
	DB      *sql.DB
	Backend Backend
	dsn     string
}

// OpenTestDB opens an isolated test database for the requested backend.
// For SQLite, creates an isolated temporary file with foreign keys and WAL mode.
// For MariaDB, connects to the ephemeral database instance provided via WACALLS_TEST_MARIADB_DSN.
func OpenTestDB(t *testing.T, backend Backend) *TestDB {
	t.Helper()

	switch backend {
	case BackendSQLite:
		dbPath := filepath.Join(t.TempDir(), "contract_test.db")
		dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)&_pragma=journal_mode(WAL)", filepath.ToSlash(dbPath))

		db, err := sql.Open("sqlite", dsn)
		if err != nil {
			t.Fatalf("testdb: failed to open sqlite database: %v", err)
		}
		db.SetMaxOpenConns(10)
		db.SetMaxIdleConns(5)
		db.SetConnMaxLifetime(5 * time.Minute)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := db.PingContext(ctx); err != nil {
			db.Close()
			t.Fatalf("testdb: failed to ping sqlite database: %v", err)
		}

		t.Cleanup(func() {
			_ = db.Close()
		})

		return &TestDB{
			DB:      db,
			Backend: BackendSQLite,
			dsn:     dsn,
		}

	case BackendMariaDB:
		dsn := os.Getenv("WACALLS_TEST_MARIADB_DSN")
		if strings.TrimSpace(dsn) == "" {
			t.Fatal("testdb: WACALLS_TEST_MARIADB_DSN environment variable is required for MariaDB tests")
		}

		db, err := sql.Open("mysql", dsn)
		if err != nil {
			t.Fatalf("testdb: failed to open mariadb database: %v", err)
		}
		db.SetMaxOpenConns(20)
		db.SetMaxIdleConns(10)
		db.SetConnMaxLifetime(5 * time.Minute)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := db.PingContext(ctx); err != nil {
			db.Close()
			t.Fatalf("testdb: failed to ping mariadb database: %v", err)
		}

		t.Cleanup(func() {
			_ = db.Close()
		})

		return &TestDB{
			DB:      db,
			Backend: BackendMariaDB,
			dsn:     dsn,
		}

	default:
		t.Fatalf("testdb: unsupported backend %q", backend)
		return nil
	}
}

// OpenSecondary opens an additional, independent connection pool to the same
// test database. This is used to test concurrency across separate connections/pools.
func (tdb *TestDB) OpenSecondary(t *testing.T) *sql.DB {
	t.Helper()

	driverName := "sqlite"
	if tdb.Backend == BackendMariaDB {
		driverName = "mysql"
	}

	secDB, err := sql.Open(driverName, tdb.dsn)
	if err != nil {
		t.Fatalf("testdb: failed to open secondary connection pool for %s: %v", tdb.Backend, err)
	}
	secDB.SetMaxOpenConns(10)
	secDB.SetMaxIdleConns(5)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := secDB.PingContext(ctx); err != nil {
		secDB.Close()
		t.Fatalf("testdb: failed to ping secondary connection pool for %s: %v", tdb.Backend, err)
	}

	t.Cleanup(func() {
		_ = secDB.Close()
	})

	return secDB
}

// IsUniqueViolation returns true if the error represents a unique constraint violation
// in either SQLite or MariaDB/MySQL.
func IsUniqueViolation(err error) bool {
	if err == nil {
		return false
	}

	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) {
		return mysqlErr.Number == 1062
	}

	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "unique constraint failed") ||
		strings.Contains(msg, "duplicate entry") ||
		strings.Contains(msg, "error 1062") ||
		strings.Contains(msg, "1062:") {
		return true
	}

	return false
}

// GetSmokeContractDDL returns the DDL for the smoke test contract table
// tailored to the respective backend dialect.
func GetSmokeContractDDL(backend Backend) string {
	switch backend {
	case BackendMariaDB:
		return `CREATE TABLE IF NOT EXISTS harness_contract_items (
			id VARCHAR(64) PRIMARY KEY,
			tenant_id VARCHAR(64) NOT NULL,
			item_key VARCHAR(128) NOT NULL,
			state VARCHAR(32) NOT NULL,
			winner_id VARCHAR(64),
			version INT NOT NULL DEFAULT 1,
			created_at BIGINT NOT NULL,
			updated_at BIGINT NOT NULL,
			CONSTRAINT uq_tenant_item_key UNIQUE (tenant_id, item_key)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;`

	default: // SQLite
		return `CREATE TABLE IF NOT EXISTS harness_contract_items (
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL,
			item_key TEXT NOT NULL,
			state TEXT NOT NULL,
			winner_id TEXT,
			version INTEGER NOT NULL DEFAULT 1,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			CONSTRAINT uq_tenant_item_key UNIQUE (tenant_id, item_key)
		);`
	}
}
