// Package database opens the SQLite database and applies schema migrations.
// It uses the pure-Go modernc.org/sqlite driver so Murmur builds without cgo.
package database

import (
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver

	"github.com/alliebayless/murmur/migrations"
)

// DB wraps *sql.DB with Murmur's schema management.
type DB struct {
	*sql.DB
	path string
}

// Open opens (creating if necessary) the database at path and migrates it to
// the latest schema version.
func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	// _txlock=immediate avoids "database is locked" surprises when two Murmur
	// processes write at once; busy_timeout makes contention wait instead of
	// failing outright.
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)", path)
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	sqlDB.SetMaxOpenConns(1) // SQLite + a single-user CLI: serialise access.

	if err := sqlDB.Ping(); err != nil {
		_ = sqlDB.Close()
		return nil, describeOpenError(path, err)
	}

	db := &DB{DB: sqlDB, path: path}
	if err := db.migrate(); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	return db, nil
}

func describeOpenError(path string, err error) error {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "malformed") || strings.Contains(msg, "not a database"):
		return fmt.Errorf("the Murmur database at %s looks corrupted.\nDelete it and run `murmur index --rebuild` to start fresh (capture history will be lost): %w", path, err)
	case strings.Contains(msg, "locked") || strings.Contains(msg, "busy"):
		return fmt.Errorf("the Murmur database at %s is locked by another process.\nClose other Murmur instances and try again: %w", path, err)
	case errors.Is(err, os.ErrPermission):
		return fmt.Errorf("no permission to open %s: %w", path, err)
	}
	return fmt.Errorf("open database %s: %w", path, err)
}

// Path returns the database file location.
func (db *DB) Path() string { return db.path }

func (db *DB) migrate() error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version    TEXT PRIMARY KEY,
		applied_at INTEGER NOT NULL
	)`); err != nil {
		return fmt.Errorf("create migration table: %w", err)
	}

	applied := map[string]bool{}
	rows, err := db.Query(`SELECT version FROM schema_migrations`)
	if err != nil {
		return fmt.Errorf("read applied migrations: %w", err)
	}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			_ = rows.Close()
			return err
		}
		applied[v] = true
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()

	entries, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		return fmt.Errorf("list migrations: %w", err)
	}
	sort.Strings(entries)

	for _, name := range entries {
		if applied[name] {
			continue
		}
		body, err := migrations.FS.ReadFile(name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", name, err)
		}
		if _, err := tx.Exec(string(body)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
			name, time.Now().Unix()); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %s: %w", name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", name, err)
		}
	}
	return nil
}

// DefaultPath returns the database location inside dataDir.
func DefaultPath(dataDir string) string {
	return filepath.Join(dataDir, "murmur.db")
}
