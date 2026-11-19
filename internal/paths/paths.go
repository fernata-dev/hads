// Package paths resolves every on-disk location Hads uses, per OS.
package paths

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// DataDir is where config, history, the file index and the socket live.
//
//	Linux:   ~/.config/hads
//	macOS:   ~/Library/Application Support/hads
//	Windows: %AppData%\hads
func DataDir() string {
	if d := os.Getenv("HADS_HOME"); d != "" {
		return d
	}
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		base = filepath.Join(Home(), ".config")
	}
	return filepath.Join(base, "hads")
}

func Ensure() error { return os.MkdirAll(DataDir(), 0o700) }

func ConfigFile() string  { return filepath.Join(DataDir(), "config.json") }
func HistoryFile() string { return filepath.Join(DataDir(), "history.jsonl") }
func IndexFile() string   { return filepath.Join(DataDir(), "index.gob") }
func LogFile() string     { return filepath.Join(DataDir(), "daemon.log") }
func PidFile() string     { return filepath.Join(DataDir(), "daemon.pid") }
func AddrFile() string    { return filepath.Join(DataDir(), "daemon.addr") }
func ImportMark() string  { return filepath.Join(DataDir(), ".imported") }
func SpawnMark() string   { return filepath.Join(DataDir(), ".spawn") }

// SocketPath is the unix socket used on Linux/macOS/BSD.
func SocketPath() string {
	p := filepath.Join(DataDir(), "hads.sock")
	if len(p) > 100 { // sun_path limit (104 on macOS)
		p = filepath.Join(os.TempDir(), fmt.Sprintf("hads-%d.sock", os.Getuid()))
	}
	return p
}

func Home() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return h
}

// Abbrev turns /home/me/x into ~/x for display and suggestions.
func Abbrev(p string) string {
	h := Home()
	if p == h {
		return "~"
	}
	if strings.HasPrefix(p, h+string(filepath.Separator)) {
		return "~" + p[len(h):]
	}
	return p
}

// Expand resolves a leading ~ to the home directory.
func Expand(p string) string {
	if p == "~" {
		return Home()
	}
	if strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		return filepath.Join(Home(), p[2:])
	}
	return p
}

// Resolve makes p absolute relative to cwd (expanding ~).
func Resolve(cwd, p string) string {
	p = Expand(p)
	if !filepath.IsAbs(p) {
		p = filepath.Join(cwd, p)
	}
	return filepath.Clean(p)
}

func IsWindows() bool { return runtime.GOOS == "windows" }

// Exe returns the absolute path of the running hads binary.
func Exe() string {
	p, err := os.Executable()
	if err != nil {
		return "hads"
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		p = r
	}
	return p
}
