package metrics

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// FS reads files below Root ("/" in production, a fixture tree in tests), so providers can be tested with the
// exact /proc and /sys layouts of multi-socket, VM, no-GPU and no-RAPL hosts.
type FS struct{ Root string }

func (f FS) path(p string) string {
	if f.Root == "" {
		return p
	}
	return filepath.Join(f.Root, p)
}

// Read returns the file content.
func (f FS) Read(p string) (string, error) {
	b, err := os.ReadFile(f.path(p))
	return string(b), err
}

// ReadTrim is Read with surrounding whitespace removed ("" on error).
func (f FS) ReadTrim(p string) string {
	s, err := f.Read(p)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(s)
}

// ReadInt parses a decimal file.
func (f FS) ReadInt(p string) (int64, bool) {
	v, err := strconv.ParseInt(f.ReadTrim(p), 10, 64)
	return v, err == nil
}

// Dir lists the entry names of a directory (nil on error).
func (f FS) Dir(p string) []string {
	es, err := os.ReadDir(f.path(p))
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(es))
	for _, e := range es {
		out = append(out, e.Name())
	}
	return out
}

// Exists reports whether p exists.
func (f FS) Exists(p string) bool { _, err := os.Stat(f.path(p)); return err == nil }
