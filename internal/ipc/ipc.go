// Package ipc is the tiny JSON-lines transport between shells/CLI and the daemon.
// Unix-likes use a 0600 unix socket; Windows uses loopback TCP + a per-user token.
package ipc

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"time"

	"hads/internal/proto"
)

var ErrNoDaemon = errors.New("hads daemon is not running")

// Call sends one request and waits for one response.
func Call(req proto.Request, timeout time.Duration) (proto.Response, error) {
	var resp proto.Response
	conn, token, err := dial(250 * time.Millisecond)
	if err != nil {
		return resp, ErrNoDaemon
	}
	defer conn.Close()
	req.Token = token
	_ = conn.SetDeadline(time.Now().Add(timeout))
	b, _ := json.Marshal(req)
	b = append(b, '\n')
	if _, err := conn.Write(b); err != nil {
		return resp, err
	}
	r := bufio.NewReaderSize(conn, 64*1024)
	line, err := r.ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return resp, err
	}
	if err := json.Unmarshal(line, &resp); err != nil {
		return resp, err
	}
	if !resp.OK && resp.Error != "" {
		return resp, errors.New(resp.Error)
	}
	return resp, nil
}

// Alive reports whether a daemon answers a ping.
func Alive() bool {
	_, err := Call(proto.Request{Op: "ping"}, 400*time.Millisecond)
	return err == nil
}

// Listener wraps the platform listener plus the expected auth token.
type Listener struct {
	net.Listener
	Token string
}

// Serve reads one request per connection and writes one response.
func Serve(l *Listener, handle func(proto.Request) proto.Response) error {
	for {
		c, err := l.Accept()
		if err != nil {
			return err
		}
		go func(c net.Conn) {
			defer c.Close()
			_ = c.SetDeadline(time.Now().Add(10 * time.Second))
			r := bufio.NewReaderSize(c, 64*1024)
			line, err := r.ReadBytes('\n')
			if err != nil && len(line) == 0 {
				return
			}
			var req proto.Request
			var resp proto.Response
			if err := json.Unmarshal(line, &req); err != nil {
				resp = proto.Response{Error: "bad request"}
			} else if l.Token != "" && req.Token != l.Token {
				resp = proto.Response{Error: "unauthorized"}
			} else {
				resp = handle(req)
			}
			b, _ := json.Marshal(resp)
			_, _ = c.Write(append(b, '\n'))
		}(c)
	}
}
