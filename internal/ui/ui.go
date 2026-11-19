// Package ui has terminal helpers: color, spinner, prompts that work even when stdin is piped.
package ui

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
	"unicode"
)

var color = func() bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	fi, err := os.Stdout.Stat()
	if err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	enableVT()
	return true
}()

func paint(code, s string) string {
	if !color {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func Bold(s string) string   { return paint("1", s) }
func Dim(s string) string    { return paint("2", s) }
func Cyan(s string) string   { return paint("38;5;87", s) }
func Violet(s string) string { return paint("38;5;141", s) }
func Green(s string) string  { return paint("38;5;114", s) }
func Yellow(s string) string { return paint("38;5;221", s) }
func Red(s string) string    { return paint("38;5;203", s) }
func Gray(s string) string   { return paint("38;5;245", s) }

// IsArabic reports whether s contains Arabic script.
func IsArabic(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Arabic, r) {
			return true
		}
	}
	return false
}

// Lang picks ar/en from config override, then env.
func Lang(override string) string {
	if override == "ar" || override == "en" {
		return override
	}
	for _, k := range []string{"HADS_LANG", "LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := os.Getenv(k); v != "" {
			if strings.HasPrefix(v, "ar") {
				return "ar"
			}
			return "en"
		}
	}
	return "en"
}

// Spinner shows a small animated status line on stderr.
type Spinner struct {
	stop chan struct{}
	wg   sync.WaitGroup
	mu   sync.Mutex
	msg  string
}

func Spin(msg string) *Spinner {
	s := &Spinner{stop: make(chan struct{}), msg: msg}
	if !color {
		fmt.Fprintln(os.Stderr, msg)
		return s
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		t := time.NewTicker(80 * time.Millisecond)
		defer t.Stop()
		for i := 0; ; i++ {
			s.mu.Lock()
			m := s.msg
			s.mu.Unlock()
			fmt.Fprintf(os.Stderr, "\r\x1b[2K %s %s", Violet(frames[i%len(frames)]), Gray(m))
			select {
			case <-s.stop:
				fmt.Fprint(os.Stderr, "\r\x1b[2K")
				return
			case <-t.C:
			}
		}
	}()
	return s
}

func (s *Spinner) Set(msg string) { s.mu.Lock(); s.msg = msg; s.mu.Unlock() }

func (s *Spinner) Stop() {
	select {
	case <-s.stop:
	default:
		close(s.stop)
	}
	s.wg.Wait()
}

// Ask reads one line from the controlling terminal (works under `curl | sh`).
func Ask(prompt string) string {
	fmt.Fprint(os.Stderr, prompt)
	in, closeFn := tty()
	defer closeFn()
	line, _ := bufio.NewReader(in).ReadString('\n')
	return strings.TrimSpace(line)
}

// AskSecret reads without echo when possible.
func AskSecret(prompt string) string {
	fmt.Fprint(os.Stderr, prompt)
	in, closeFn := tty()
	defer closeFn()
	restore := noEcho(in)
	line, _ := bufio.NewReader(in).ReadString('\n')
	restore()
	fmt.Fprintln(os.Stderr)
	return strings.TrimSpace(line)
}

// Interactive reports whether we can talk to a human.
func Interactive() bool {
	in, closeFn := tty()
	defer closeFn()
	return in != nil && in != os.Stdin || isCharDevice(os.Stdin)
}

func isCharDevice(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// Rule prints a thin divider sized for most terminals.
func Rule() string { return Gray(strings.Repeat("─", 58)) }
