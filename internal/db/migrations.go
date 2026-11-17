package db

import (
	"embed"
	"io/fs"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Migrations returns the daemon database migrations (yubeos.db).
func Migrations() fs.FS {
	sub, _ := fs.Sub(migrationFiles, "migrations")
	return sub
}
