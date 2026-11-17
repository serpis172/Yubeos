// Package auth is the daemon-side authentication service: login, sessions, lockout and bans
// (docs/AUTHENTICATION.md). Credential material stays in the root helper.
package auth

import (
	"github.com/serpis172/Yubeos/internal/identity"
	"github.com/serpis172/Yubeos/internal/netx"
)

// NeedsSecondFactor implements docs/AUTHENTICATION.md §4: a remote origin ALWAYS needs one, whatever the setting;
// otherwise auth.require2fa decides ("none" | "admins" | "all").
func NeedsSecondFactor(require2fa string, role identity.Role, origin netx.Origin) bool {
	if origin == netx.Remote {
		return true
	}
	switch require2fa {
	case "all":
		return true
	case "admins":
		return role == identity.Admin
	default:
		return false
	}
}
