package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"strings"
	"time"

	"github.com/serpis172/Yubeos/internal/audit"
	"github.com/serpis172/Yubeos/internal/identity"
	"github.com/serpis172/Yubeos/internal/netx"
)

// DeviceCookie carries the trusted-device token (docs/AUTHENTICATION.md §5).
const DeviceCookie = "__Host-yubeos_device"

const deviceGrace = 30 * time.Second // the previous token stays valid briefly after rotation (parallel requests)

// Device is the listing view of a trusted device.
type Device struct {
	ID         string
	Label      string
	Browser    string
	OS         string
	LastIP     string
	Current    bool
	CreatedAt  time.Time
	LastUsedAt time.Time
	ExpiresAt  time.Time
}

// uaBits is a coarse (family, os) classification used to bind a device to its browser. Never a security boundary
// by itself: it only invalidates trust when the browser family or OS changes.
func uaBits(ua string) (family, osName string) {
	l := strings.ToLower(ua)
	switch {
	case strings.Contains(l, "edg/"):
		family = "Edge"
	case strings.Contains(l, "firefox/"):
		family = "Firefox"
	case strings.Contains(l, "chrome/") || strings.Contains(l, "crios/"):
		family = "Chrome"
	case strings.Contains(l, "safari/"):
		family = "Safari"
	default:
		family = "Other"
	}
	switch {
	case strings.Contains(l, "android"):
		osName = "Android"
	case strings.Contains(l, "iphone") || strings.Contains(l, "ipad"):
		osName = "iOS"
	case strings.Contains(l, "windows"):
		osName = "Windows"
	case strings.Contains(l, "mac os x") || strings.Contains(l, "macintosh"):
		osName = "macOS"
	case strings.Contains(l, "linux") || strings.Contains(l, "x11"):
		osName = "Linux"
	default:
		osName = "Other"
	}
	return
}

func randID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// trustDays is the lifetime granted to a new trusted device (0: the feature is off).
func (s *Service) trustDays(role identity.Role, origin netx.Origin) int {
	d := s.cfg.TrustedDeviceDays
	if d > 90 {
		d = 90
	}
	if role == identity.Admin && origin == netx.Remote && s.cfg.AdminRemoteTrustedDeviceDays > 0 && d > s.cfg.AdminRemoteTrustedDeviceDays {
		d = s.cfg.AdminRemoteTrustedDeviceDays
	}
	return d
}

// createDevice stores a new trusted device and returns its cookie value and id.
func (s *Service) createDevice(ctx context.Context, user string, role identity.Role, c Client) (token, id string, err error) {
	days := s.trustDays(role, c.Origin)
	if days <= 0 {
		return "", "", nil
	}
	now := s.clk.Now()
	token, id = newSecret(32), randID()
	fam, osn := uaBits(c.UserAgent)
	_, err = s.Sessions.db.ExecContext(ctx, `INSERT INTO trusted_devices(id, username, token_hash, label, ua_family, ua_major, os_family,
		created_at, last_used_at, last_ip, last_2fa_at, expires_at) VALUES(?,?,?,?,?,0,?,?,?,?,?,?)`,
		id, user, hashTok(token), fam+" on "+osn, fam, osn, now.UnixMilli(), now.UnixMilli(), c.IP, now.UnixMilli(),
		now.Add(time.Duration(days)*24*time.Hour).UnixMilli())
	if err != nil {
		return "", "", err
	}
	s.auditEv(ctx, c, user, role, "auth.device.new", id, audit.OK, map[string]any{"days": days})
	return token, id, nil
}

// checkDevice validates a trusted-device cookie for user. newToken is the rotated cookie value (empty: keep the
// current one). Presenting an already-rotated token after the grace period is treated as theft.
func (s *Service) checkDevice(ctx context.Context, user, token string, c Client) (id, newToken string, ok bool) {
	if len(token) < 32 || len(token) > 128 {
		return "", "", false
	}
	h := hashTok(token)
	now := s.clk.Now().UnixMilli()
	var (
		uname, fam, osn string
		exp             int64
		revoked         sql.NullInt64
		cur             []byte
		prevUntil       sql.NullInt64
		famN, osN       sql.NullString
	)
	err := s.Sessions.db.QueryRowContext(ctx, `SELECT id, username, ua_family, os_family, expires_at, revoked_at, token_hash, prev_valid_until
		FROM trusted_devices WHERE token_hash=? OR prev_token_hash=?`, h, h).Scan(&id, &uname, &famN, &osN, &exp, &revoked, &cur, &prevUntil)
	if err != nil || uname != user || revoked.Valid || exp <= now {
		return "", "", false
	}
	fam, osn = famN.String, osN.String
	if f, o := uaBits(c.UserAgent); f != fam || o != osn {
		return "", "", false
	}
	if string(cur) != string(h) { // old (already rotated) token
		if prevUntil.Valid && prevUntil.Int64 > now {
			return id, "", true
		}
		_ = s.revokeDevice(ctx, user, id, "reuse-detected")
		s.auditEv(ctx, c, user, "", "auth.device.reuse", id, audit.Denied, nil)
		s.log.Warn("trusted device token reuse detected: device and its sessions revoked", "user", sanitizeLog(user), "device", id)
		return "", "", false
	}
	newToken = newSecret(32)
	_, err = s.Sessions.db.ExecContext(ctx, `UPDATE trusted_devices SET token_hash=?, prev_token_hash=?, prev_valid_until=?, last_used_at=?, last_ip=? WHERE id=?`,
		hashTok(newToken), h, s.clk.Now().Add(deviceGrace).UnixMilli(), now, c.IP, id)
	if err != nil {
		return "", "", false
	}
	return id, newToken, true
}

func (s *Service) revokeDevice(ctx context.Context, user, id, reason string) error {
	res, err := s.Sessions.db.ExecContext(ctx, `UPDATE trusted_devices SET revoked_at=?, revoke_reason=? WHERE id=? AND username=? AND revoked_at IS NULL`,
		s.clk.Now().UnixMilli(), reason, id, user)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return s.Sessions.RevokeByDevice(ctx, id)
	}
	return nil
}

// RevokeDevice revokes one of user's trusted devices; false when it does not exist.
func (s *Service) RevokeDevice(ctx context.Context, user, id string, c Client) (bool, error) {
	var n int
	if err := s.Sessions.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM trusted_devices WHERE id=? AND username=? AND revoked_at IS NULL`, id, user).Scan(&n); err != nil || n == 0 {
		return false, err
	}
	err := s.revokeDevice(ctx, user, id, "user")
	s.auditEv(ctx, c, user, "", "auth.device.revoke", id, audit.OK, nil)
	return err == nil, err
}

// RevokeAllDevices revokes every trusted device of user (password change, 2FA reset, recovery-code use…).
func (s *Service) RevokeAllDevices(ctx context.Context, user, reason string) error {
	rows, err := s.Sessions.db.QueryContext(ctx, `SELECT id FROM trusted_devices WHERE username=? AND revoked_at IS NULL`, user)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	_ = rows.Close()
	var first error
	for _, id := range ids {
		if err := s.revokeDevice(ctx, user, id, reason); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// ListDevices lists user's active trusted devices.
func (s *Service) ListDevices(ctx context.Context, user, currentID string) ([]Device, error) {
	rows, err := s.Sessions.db.QueryContext(ctx, `SELECT id, label, ua_family, os_family, last_ip, created_at, last_used_at, expires_at
		FROM trusted_devices WHERE username=? AND revoked_at IS NULL AND expires_at>? ORDER BY last_used_at DESC`, user, s.clk.Now().UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Device
	for rows.Next() {
		var (
			d             Device
			fam, osn, ip  sql.NullString
			created, used int64
			exp           int64
		)
		if err := rows.Scan(&d.ID, &d.Label, &fam, &osn, &ip, &created, &used, &exp); err != nil {
			return nil, err
		}
		d.Browser, d.OS, d.LastIP = fam.String, osn.String, ip.String
		d.CreatedAt, d.LastUsedAt, d.ExpiresAt = time.UnixMilli(created), time.UnixMilli(used), time.UnixMilli(exp)
		d.Current = d.ID == currentID
		out = append(out, d)
	}
	return out, rows.Err()
}
