//go:build !windows

package ipc

import (
	"errors"
	"net"
	"os"
	"time"

	"hads/internal/paths"
)

func dial(timeout time.Duration) (net.Conn, string, error) {
	c, err := net.DialTimeout("unix", paths.SocketPath(), timeout)
	return c, "", err
}

// Listen claims the socket; it fails if another live daemon owns it.
func Listen() (*Listener, error) {
	sock := paths.SocketPath()
	if _, err := os.Stat(sock); err == nil {
		if c, err := net.DialTimeout("unix", sock, 300*time.Millisecond); err == nil {
			c.Close()
			return nil, errors.New("another hads daemon is already running")
		}
		_ = os.Remove(sock) // stale
	}
	old := umask(0o077)
	l, err := net.Listen("unix", sock)
	umask(old)
	if err != nil {
		return nil, err
	}
	_ = os.Chmod(sock, 0o600)
	return &Listener{Listener: l}, nil
}

func Cleanup() { _ = os.Remove(paths.SocketPath()) }
