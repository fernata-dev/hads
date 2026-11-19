// Package daemon is the long-running brain: it owns the model, the file index and the socket.
package daemon

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"hads/internal/config"
	"hads/internal/history"
	"hads/internal/index"
	"hads/internal/ipc"
	"hads/internal/model"
	"hads/internal/paths"
	"hads/internal/proto"
)

type Server struct {
	cfg     *config.Config
	model   *model.Model
	index   *index.Index
	hist    *history.Store
	started time.Time
	logger  *log.Logger
	quit    chan struct{}
}

func openLog() *log.Logger {
	p := paths.LogFile()
	if fi, err := os.Stat(p); err == nil && fi.Size() > 4<<20 {
		_ = os.Rename(p, p+".1")
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return log.New(io.Discard, "", 0)
	}
	return log.New(f, "", log.LstdFlags)
}

// Run blocks serving requests until shutdown.
func Run() error {
	if err := paths.Ensure(); err != nil {
		return err
	}
	cfg, _ := config.Load()
	s := &Server{
		cfg:     cfg,
		model:   model.New(),
		index:   index.New(),
		hist:    history.NewStore(paths.HistoryFile()),
		started: time.Now(),
		logger:  openLog(),
		quit:    make(chan struct{}),
	}
	l, err := ipc.Listen()
	if err != nil {
		return err
	}
	defer ipc.Cleanup()
	_ = os.WriteFile(paths.PidFile(), []byte(strconv.Itoa(os.Getpid())), 0o600)
	defer os.Remove(paths.PidFile())
	lowerPriority()

	t0 := time.Now()
	entries, _ := history.Load(paths.HistoryFile())
	for _, e := range entries {
		s.model.Record(e)
	}
	s.logger.Printf("started pid=%d, replayed %d commands in %s", os.Getpid(), len(entries), time.Since(t0))

	if err := s.index.Load(paths.IndexFile()); err != nil {
		s.logger.Printf("no index snapshot yet: %v", err)
	}
	go s.indexLoop()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case <-sig:
		case <-s.quit:
		}
		s.logger.Printf("shutting down")
		l.Close()
	}()
	err = ipc.Serve(l, s.handle)
	select {
	case <-s.quit:
		return nil
	default:
	}
	if strings.Contains(fmt.Sprint(err), "closed") {
		return nil
	}
	return err
}

func (s *Server) reindex() {
	t := time.Now()
	_ = s.index.Scan(s.cfg)
	_ = s.index.Save(paths.IndexFile())
	s.logger.Printf("indexed %d entries in %s", s.index.Len(), time.Since(t).Round(time.Millisecond))
}

func (s *Server) indexLoop() {
	every := time.Duration(s.cfg.RescanMinutes) * time.Minute
	if s.index.Len() == 0 || time.Since(s.index.Built()) > every {
		time.Sleep(2 * time.Second)
		s.reindex()
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			s.reindex()
		case <-s.quit:
			return
		}
	}
}

func (s *Server) handle(r proto.Request) proto.Response {
	switch r.Op {
	case "ping":
		return proto.Response{OK: true}
	case "predict":
		sugg := s.model.Predict(model.Req{Buffer: r.Buffer, Cwd: r.Cwd, Sess: r.Sess, Shell: r.Shell, Lang: r.Lang, Limit: r.Limit}, s.index)
		return proto.Response{OK: true, Suggestions: sugg}
	case "record":
		e := history.Entry{T: r.Time, Cmd: r.Cmd, Cwd: r.Cwd, Exit: r.Exit, Shell: r.Shell, Sess: r.Sess}
		if e.T == 0 {
			e.T = time.Now().Unix()
		}
		if !history.ShouldRecord(e.Cmd) {
			return proto.Response{OK: true}
		}
		e.Cmd = strings.TrimSpace(e.Cmd)
		s.model.Record(e)
		_ = s.hist.Append(e)
		s.learnPaths(e)
		return proto.Response{OK: true}
	case "find":
		lim := r.Limit
		if lim <= 0 {
			lim = 20
		}
		m := s.model.SearchFiles(s.index, r.Query, index.SearchOpts{Cwd: r.Cwd, Limit: lim, DirsOnly: r.Dirs})
		return proto.Response{OK: true, Matches: m}
	case "stats":
		st := s.model.Stats()
		st["indexed_entries"] = s.index.Len()
		st["index_built"] = s.index.Built().Format(time.RFC3339)
		st["index_scan_seconds"] = s.index.LastDuration().Seconds()
		st["indexing"] = s.index.Scanning()
		st["uptime_seconds"] = int(time.Since(s.started).Seconds())
		st["pid"] = os.Getpid()
		st["top_dirs"] = s.index.TopDirs(40)
		return proto.Response{OK: true, Stats: st}
	case "reindex":
		go s.reindex()
		return proto.Response{OK: true}
	case "reload":
		if c, err := config.Load(); err == nil {
			s.cfg = c
		}
		return proto.Response{OK: true}
	case "shutdown":
		go func() {
			time.Sleep(50 * time.Millisecond)
			close(s.quit)
		}()
		return proto.Response{OK: true}
	}
	return proto.Response{Error: "unknown op " + r.Op}
}

// learnPaths adds freshly created folders/files to the index without waiting for a rescan.
func (s *Server) learnPaths(e history.Entry) {
	if e.Exit != 0 || e.Cwd == "" {
		return
	}
	toks, _, _ := model.Split(e.Cmd)
	pi := model.ProgIndex(toks)
	if pi < 0 {
		return
	}
	switch model.ProgName(toks[pi]) {
	case "mkdir", "touch", "cp", "mv", "git", "wget", "curl", "tar", "unzip", "new-item", "ni":
		for _, t := range toks[pi+1:] {
			if strings.HasPrefix(t, "-") || strings.Contains(t, "://") {
				continue
			}
			p := paths.Resolve(e.Cwd, t)
			if _, err := os.Stat(p); err == nil {
				s.index.Add(p)
			}
		}
	}
}

// ---------- process control used by the CLI ----------

// Spawn starts the daemon in the background, detached from the terminal.
func Spawn() error {
	_ = paths.Ensure()
	// don't stampede: one spawn attempt per 5s across all shells
	if Paused() {
		return nil
	}
	mark := paths.SpawnMark()
	if fi, err := os.Stat(mark); err == nil && time.Since(fi.ModTime()) < 5*time.Second {
		return nil
	}
	_ = os.WriteFile(mark, nil, 0o600)
	_ = os.Chtimes(mark, time.Now(), time.Now())
	cmd := exec.Command(paths.Exe(), "daemon", "--foreground")
	cmd.Dir = paths.Home()
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// Paused is set by `hads stop` so shells don't resurrect the daemon.
func Paused() bool {
	_, err := os.Stat(filepath.Join(paths.DataDir(), ".stopped"))
	return err == nil
}

func SetPaused(p bool) {
	f := filepath.Join(paths.DataDir(), ".stopped")
	if p {
		_ = os.WriteFile(f, nil, 0o600)
	} else {
		_ = os.Remove(f)
	}
}

// EnsureRunning starts the daemon if it is not answering, waiting briefly for it.
func EnsureRunning(wait time.Duration) bool {
	if ipc.Alive() {
		return true
	}
	_ = os.Remove(paths.SpawnMark())
	if Spawn() != nil {
		return false
	}
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		time.Sleep(60 * time.Millisecond)
		if ipc.Alive() {
			return true
		}
	}
	return false
}

func Stop() bool {
	_, err := ipc.Call(proto.Request{Op: "shutdown"}, time.Second)
	if err == nil {
		for i := 0; i < 20 && ipc.Alive(); i++ {
			time.Sleep(50 * time.Millisecond)
		}
		return true
	}
	return false
}
