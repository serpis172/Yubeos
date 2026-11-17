package privd

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/user"
	"strconv"
	"syscall"
)

// ListenUnix creates the helper socket with mode 0660 and (when group != "") group ownership, without a window in
// which it is more permissive: the umask is tightened around Listen. A stale socket file from a previous run is
// removed; anything else at that path is an error.
func ListenUnix(path string, group string) (net.Listener, error) {
	if st, err := os.Lstat(path); err == nil {
		if st.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("privd: %s exists and is not a socket", path)
		}
		if err := os.Remove(path); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	old := syscall.Umask(0o117) // 0777 &^ 0117 = 0660
	ln, err := net.Listen("unix", path)
	syscall.Umask(old)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o660); err != nil {
		_ = ln.Close()
		return nil, err
	}
	if group != "" {
		g, err := user.LookupGroup(group)
		if err != nil {
			_ = ln.Close()
			return nil, fmt.Errorf("privd: group %q not found: %w", group, err)
		}
		gid, _ := strconv.Atoi(g.Gid)
		if err := os.Chown(path, -1, gid); err != nil {
			_ = ln.Close()
			return nil, fmt.Errorf("privd: chown socket to group %q: %w", group, err)
		}
	}
	return ln, nil
}
