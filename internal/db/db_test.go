package db

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func TestOpenAppliesMigrationsAndSecuresFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "y.db")
	d, err := Open(context.Background(), p, Migrations(), quiet)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	st, _ := os.Stat(p)
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("db mode %o, want 600", st.Mode().Perm())
	}
	for _, table := range []string{"settings", "users_meta", "sessions", "trusted_devices", "login_failures", "ip_bans", "api_tokens", "audit_log", "jobs", "schema_migrations"} {
		var n int
		if err := d.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&n); err != nil || n != 1 {
			t.Errorf("table %s missing", table)
		}
	}
	var fk, jm string
	_ = d.QueryRow(`PRAGMA foreign_keys`).Scan(&fk)
	_ = d.QueryRow(`PRAGMA journal_mode`).Scan(&jm)
	if fk != "1" || jm != "wal" {
		t.Fatalf("pragmas: foreign_keys=%s journal_mode=%s", fk, jm)
	}
}

func TestForeignKeysAreEnforced(t *testing.T) {
	d, _ := Open(context.Background(), filepath.Join(t.TempDir(), "y.db"), Migrations(), quiet)
	defer d.Close()
	_, err := d.Exec(`INSERT INTO sessions(id_hash,sid,username,csrf,origin,created_at,rotated_at,last_seen_at,expires_at) VALUES(x'01','s','ghost','c','local',1,1,1,1)`)
	if err == nil {
		t.Fatal("session for a non-existent user must violate the foreign key")
	}
}

func TestReopenIsIdempotentAndBacksUpBeforeNewMigration(t *testing.T) {
	p := filepath.Join(t.TempDir(), "y.db")
	migs := fstest.MapFS{"0001_a.sql": {Data: []byte(`CREATE TABLE a(x INTEGER);`)}}
	d, err := Open(context.Background(), p, migs, quiet)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = d.Exec(`INSERT INTO a VALUES(7)`)
	d.Close()

	d, err = Open(context.Background(), p, migs, quiet) // nothing pending
	if err != nil {
		t.Fatal(err)
	}
	d.Close()
	if _, err := os.Stat(p + ".pre-migrate-0001.bak"); err == nil {
		t.Fatal("no backup expected when nothing is pending")
	}

	migs["0002_b.sql"] = &fstest.MapFile{Data: []byte(`CREATE TABLE b(y INTEGER);`)}
	d, err = Open(context.Background(), p, migs, quiet)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	bak := p + ".pre-migrate-0002.bak"
	st, err := os.Stat(bak)
	if err != nil {
		t.Fatalf("pre-migration backup missing: %v", err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("backup mode %o", st.Mode().Perm())
	}
	var x int
	if err := d.QueryRow(`SELECT x FROM a`).Scan(&x); err != nil || x != 7 {
		t.Fatalf("data lost across migration: %v %d", err, x)
	}
}

func TestFailedMigrationRollsBack(t *testing.T) {
	p := filepath.Join(t.TempDir(), "y.db")
	migs := fstest.MapFS{"0001_bad.sql": {Data: []byte(`CREATE TABLE ok(x); INSERT INTO nope VALUES(1);`)}}
	if _, err := Open(context.Background(), p, migs, quiet); err == nil {
		t.Fatal("expected migration error")
	}
	migs = fstest.MapFS{"0001_good.sql": {Data: []byte(`CREATE TABLE ok(x);`)}}
	d, err := Open(context.Background(), p, migs, quiet)
	if err != nil {
		t.Fatalf("partial migration must not leave residue: %v", err)
	}
	d.Close()
}

func TestNewerSchemaIsRefused(t *testing.T) {
	p := filepath.Join(t.TempDir(), "y.db")
	newer := fstest.MapFS{"0001_a.sql": {Data: []byte(`CREATE TABLE a(x);`)}, "0002_b.sql": {Data: []byte(`CREATE TABLE b(x);`)}}
	d, _ := Open(context.Background(), p, newer, quiet)
	d.Close()
	older := fstest.MapFS{"0001_a.sql": {Data: []byte(`CREATE TABLE a(x);`)}}
	if _, err := Open(context.Background(), p, older, quiet); !errors.Is(err, ErrNewerSchema) {
		t.Fatalf("expected ErrNewerSchema, got %v", err)
	}
}

func TestDuplicateMigrationVersionRejected(t *testing.T) {
	migs := fstest.MapFS{"0001_a.sql": {Data: []byte(`SELECT 1;`)}, "0001_b.sql": {Data: []byte(`SELECT 1;`)}}
	if _, err := Open(context.Background(), filepath.Join(t.TempDir(), "y.db"), migs, quiet); err == nil {
		t.Fatal("duplicate versions must be rejected")
	}
}

func TestInTxRollsBackOnErrorAndCommitsOnSuccess(t *testing.T) {
	d, _ := Open(context.Background(), filepath.Join(t.TempDir(), "y.db"), Migrations(), quiet)
	defer d.Close()
	ctx := context.Background()
	boom := errors.New("boom")
	err := d.InTx(ctx, func(tx *sql.Tx) error {
		_, _ = tx.Exec(`INSERT INTO settings(key,value,updated_at) VALUES('k','1',1)`)
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("got %v", err)
	}
	var n int
	_ = d.QueryRow(`SELECT COUNT(*) FROM settings`).Scan(&n)
	if n != 0 {
		t.Fatal("failed transaction must roll back")
	}
	if err := d.InTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO settings(key,value,updated_at) VALUES('k','1',1)`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	_ = d.QueryRow(`SELECT COUNT(*) FROM settings`).Scan(&n)
	if n != 1 {
		t.Fatal("successful transaction must commit")
	}
}
