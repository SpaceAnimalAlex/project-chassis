package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"net/url"
	"runtime"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// DB wraps sql.DB with Chassis-specific pragmas, migrations, and transaction management.
type DB struct {
	*sql.DB
}

// Config defines connection parameters for the embedded SQLite database.
type Config struct {
	Path        string // File path, e.g. "chassis.db" or ":memory:"
	BusyTimeout int    // Milliseconds to wait on busy locks (default: 5000)
}

// Open initializes the embedded SQLite database with durable WAL pragmas.
func Open(cfg Config) (*DB, error) {
	if cfg.BusyTimeout <= 0 {
		cfg.BusyTimeout = 5000
	}

	dsn := formatDSN(cfg)
	rawDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	// Pool sizing tuned for mini-PC benchmark (WAL handles multi-readers, serialized writes)
	maxConns := max(4, runtime.NumCPU()*2)
	maxIdle := max(2, runtime.NumCPU())
	rawDB.SetMaxOpenConns(maxConns)
	rawDB.SetMaxIdleConns(maxIdle)
	rawDB.SetConnMaxLifetime(1 * time.Hour)

	// Apply critical PRAGMAs to establish durable operational mode
	initPragmas := []string{
		fmt.Sprintf("PRAGMA busy_timeout = %d;", cfg.BusyTimeout),
		"PRAGMA journal_mode = WAL;",
		"PRAGMA synchronous = NORMAL;",
		"PRAGMA foreign_keys = ON;",
		"PRAGMA cache_size = -20000;", // 20MB cache
	}

	for _, pragma := range initPragmas {
		if _, err := rawDB.Exec(pragma); err != nil {
			_ = rawDB.Close()
			return nil, fmt.Errorf("failed to execute pragma (%s): %w", pragma, err)
		}
	}

	return &DB{DB: rawDB}, nil
}

func formatDSN(cfg Config) string {
	if cfg.Path == ":memory:" {
		return "file::memory:?cache=shared&_txlock=immediate"
	}
	// Use _txlock=immediate so all standard Begin/BeginTx acquire write locks up front
	params := url.Values{}
	params.Set("_txlock", "immediate")
	params.Set("_pragma", fmt.Sprintf("busy_timeout(%d)", cfg.BusyTimeout))

	if strings.Contains(cfg.Path, "?") {
		return cfg.Path + "&" + params.Encode()
	}
	return cfg.Path + "?" + params.Encode()
}

// Migrate applies all embedded SQL migrations in lexical order.
func (db *DB) Migrate(ctx context.Context) error {
	// Create schema migrations tracker table
	createTracker := `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
	`
	if _, err := db.ExecContext(ctx, createTracker); err != nil {
		return fmt.Errorf("failed to create schema_migrations table: %w", err)
	}

	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return fmt.Errorf("failed to read embedded migrations directory: %w", err)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		version := entry.Name()
		var exists int
		checkErr := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations WHERE version = ?", version).Scan(&exists)
		if checkErr != nil {
			return fmt.Errorf("failed to check migration status for %s: %w", version, checkErr)
		}
		if exists > 0 {
			continue // Already applied
		}

		content, readErr := migrationFS.ReadFile("migrations/" + version)
		if readErr != nil {
			return fmt.Errorf("failed to read migration file %s: %w", version, readErr)
		}

		err = db.WithTx(ctx, func(tx *sql.Tx) error {
			if _, execErr := tx.ExecContext(ctx, string(content)); execErr != nil {
				return fmt.Errorf("migration %s failed: %w", version, execErr)
			}
			if _, trackErr := tx.ExecContext(ctx, "INSERT INTO schema_migrations (version) VALUES (?)", version); trackErr != nil {
				return fmt.Errorf("failed to record migration %s: %w", version, trackErr)
			}
			return nil
		})
		if err != nil {
			return err
		}
	}

	return nil
}

// WithTx executes the provided function within an IMMEDIATE transaction.
// Automatically rolls back if an error or panic occurs, and commits upon success.
func (db *DB) WithTx(ctx context.Context, fn func(tx *sql.Tx) error) (err error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p) // Re-throw panic after rollback
		} else if err != nil {
			_ = tx.Rollback()
		} else {
			err = tx.Commit()
		}
	}()

	err = fn(tx)
	return err
}
