//go:build windows

package ipc

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"os"
	"strings"
	"time"

	"hads/internal/paths"
)

func readAddr() (addr, token string, err error) {
	b, err := os.ReadFile(paths.AddrFile())
	if err != nil {
		return "", "", err
	}
	parts := strings.SplitN(strings.TrimSpace(string(b)), "\n", 2)
	if len(parts) != 2 {
		return "", "", errors.New("bad addr file")
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), nil
}

func dial(timeout time.Duration) (net.Conn, string, error) {
	addr, token, err := readAddr()
	if err != nil {
		return nil, "", err
	}
	c, err := net.DialTimeout("tcp", addr, timeout)
	return c, token, err
}

func Listen() (*Listener, error) {
	if c, _, err := dial(300 * time.Millisecond); err == nil {
		c.Close()
		return nil, errors.New("another hads daemon is already running")
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	buf := make([]byte, 24)
	_, _ = rand.Read(buf)
	token := hex.EncodeToString(buf)
	if err := os.WriteFile(paths.AddrFile(), []byte(l.Addr().String()+"\n"+token+"\n"), 0o600); err != nil {
		l.Close()
		return nil, err
	}
	return &Listener{Listener: l, Token: token}, nil
}

func Cleanup() { _ = os.Remove(paths.AddrFile()) }
