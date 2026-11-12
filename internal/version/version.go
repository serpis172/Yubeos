// Package version holds build metadata, overridden at link time:
//
//	-ldflags "-X github.com/serpis172/Yubeos/internal/version.Version=1.2.3 -X ...Commit=abc123"
package version

var (
	Version = "0.0.0-dev"
	Commit  = "unknown"
)
