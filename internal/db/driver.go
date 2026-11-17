package db

// The only place that names the SQLite driver: pure Go (no cgo), ADR-0005 / ADR-0013.
import _ "modernc.org/sqlite"

const driverName = "sqlite"
