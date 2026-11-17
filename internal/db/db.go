// Package db opens SQLite (WAL, foreign keys, 0600) and applies embedded forward-only migrations.
// Rules: docs/DATA-MODEL.md §1 and §4.
package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ErrNewerSchema is returned when the database was migrated by a newer Yubeos than this binary.
var ErrNewerSchema = errors.New("db: database schema is newer than this binary supports")

// DB wraps *sql.DB. It uses ONE connection: SQLite allows a single writer anyway, and a single connection
// makes per-connection PRAGMAs (foreign_keys, busy_timeout) reliable and transactions serial.
type DB struct {
	*sql.DB
	path string
}

var migrationName = regexp.MustCompile(`^(\d{4})_[a-z0-9_]+\.sql$`)

// Open creates the file with mode 0600 if needed, configures SQLite and applies pending migrations from migs
// (files named NNNN_name.sql at the root of migs). Before applying migrations to an existing database a
// consistent backup <path>.pre-migrate-<version>.bak is written.
func Open(ctx context.Context, path string, migs fs.FS, log *slog.Logger) (*DB, error) {
	if log == nil {
		log = slog.Default()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("db: create dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("db: create file: %w", err)
	}
	_ = f.Close()

	sqldb, err := sql.Open(driverName, path)
	if err != nil {
		return nil, fmt.Errorf("db: open: %w", err)
	}
	sqldb.SetMaxOpenConns(1)
	sqldb.SetConnMaxLifetime(0)
	d := &DB{DB: sqldb, path: path}
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA foreign_keys=ON",
		"PRAGMA busy_timeout=5000",
		"PRAGMA synchronous=NORMAL",
	} {
		if _, err := d.ExecContext(ctx, pragma); err != nil {
			_ = sqldb.Close()
			return nil, fmt.Errorf("db: %s: %w", pragma, err)
		}
	}
	if err := d.migrate(ctx, migs, log); err != nil {
		_ = sqldb.Close()
		return nil, err
	}
	return d, nil
}

// OpenReadOnly opens an existing database without creating, migrating or modifying it (used by yubeosctl).
func OpenReadOnly(ctx context.Context, path string) (*DB, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	sqldb, err := sql.Open(driverName, "file:"+path+"?mode=ro")
	if err != nil {
		return nil, err
	}
	sqldb.SetMaxOpenConns(1)
	if _, err := sqldb.ExecContext(ctx, "PRAGMA busy_timeout=5000"); err != nil {
		_ = sqldb.Close()
		return nil, err
	}
	return &DB{DB: sqldb, path: path}, nil
}

// Path returns the database file path.
func (d *DB) Path() string { return d.path }

type migration struct {
	version int
	name    string
	sql     string
}

func loadMigrations(migs fs.FS) ([]migration, error) {
	entries, err := fs.ReadDir(migs, ".")
	if err != nil {
		return nil, fmt.Errorf("db: read migrations: %w", err)
	}
	var out []migration
	for _, e := range entries {
		m := migrationName.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		v, _ := strconv.Atoi(m[1])
		b, err := fs.ReadFile(migs, e.Name())
		if err != nil {
			return nil, err
		}
		out = append(out, migration{version: v, name: e.Name(), sql: string(b)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	for i := 1; i < len(out); i++ {
		if out[i].version == out[i-1].version {
			return nil, fmt.Errorf("db: duplicate migration version %04d", out[i].version)
		}
	}
	return out, nil
}

func (d *DB) migrate(ctx context.Context, migs fs.FS, log *slog.Logger) error {
	all, err := loadMigrations(migs)
	if err != nil {
		return err
	}
	if _, err := d.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations(version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL)`); err != nil {
		return fmt.Errorf("db: schema_migrations: %w", err)
	}
	var current sql.NullInt64
	if err := d.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&current); err != nil {
		return err
	}
	latest := 0
	if len(all) > 0 {
		latest = all[len(all)-1].version
	}
	if int(current.Int64) > latest {
		return fmt.Errorf("%w (database at %d, binary at %d)", ErrNewerSchema, current.Int64, latest)
	}
	var pending []migration
	for _, m := range all {
		if m.version > int(current.Int64) {
			pending = append(pending, m)
		}
	}
	if len(pending) == 0 {
		return nil
	}
	if current.Valid && current.Int64 > 0 {
		bak := fmt.Sprintf("%s.pre-migrate-%04d.bak", d.path, pending[0].version)
		_ = os.Remove(bak)
		if err := d.Backup(ctx, bak); err != nil {
			return fmt.Errorf("db: pre-migration backup: %w", err)
		}
		log.Info("database backed up before migration", "file", bak)
	}
	for _, m := range pending {
		tx, err := d.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, m.sql); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("db: migration %s: %w", m.name, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version, applied_at) VALUES(?, ?)`, m.version, time.Now().UnixMilli()); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		log.Info("migration applied", "version", m.version, "name", strings.TrimSuffix(m.name, ".sql"))
	}
	return nil
}

// Backup writes a consistent copy (VACUUM INTO) with mode 0600. dest must not exist.
func (d *DB) Backup(ctx context.Context, dest string) error {
	if _, err := d.ExecContext(ctx, `VACUUM INTO ?`, dest); err != nil {
		return err
	}
	return os.Chmod(dest, 0o600)
}

// InTx runs fn in a transaction, rolling back on error or panic.
func (d *DB) InTx(ctx context.Context, fn func(tx *sql.Tx) error) (err error) {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
	}()
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
