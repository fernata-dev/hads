// Package history stores executed commands (JSONL) and imports existing shell histories.
package history

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"hads/internal/paths"
)

type Entry struct {
	T     int64  `json:"t"`
	Cmd   string `json:"c"`
	Cwd   string `json:"d,omitempty"`
	Exit  int    `json:"x"`
	Shell string `json:"s,omitempty"`
	Sess  string `json:"p,omitempty"`
}

type Store struct {
	mu   sync.Mutex
	path string
}

func NewStore(path string) *Store { return &Store{path: path} }

func (s *Store) Append(e Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	b, _ := json.Marshal(e)
	_, err = f.Write(append(b, '\n'))
	return err
}

// AppendDirect is used by the CLI when the daemon is down, so nothing is lost.
func AppendDirect(e Entry) error {
	if err := paths.Ensure(); err != nil {
		return err
	}
	return NewStore(paths.HistoryFile()).Append(e)
}

func Load(path string) ([]Entry, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var out []Entry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		var e Entry
		if json.Unmarshal(sc.Bytes(), &e) == nil && e.Cmd != "" {
			out = append(out, e)
		}
	}
	return out, sc.Err()
}

var secretRe = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(pass(word|wd)?|secret|token|api[_-]?key|access[_-]?key|private[_-]?key|auth)\s*[=:]\s*\S{6,}`),
	regexp.MustCompile(`(?i)--?(pass(word)?|token|secret|api-?key)[= ]\S{6,}`),
	regexp.MustCompile(`(?i)(authorization|x-api-key)\s*:\s*\S+`),
	regexp.MustCompile(`\b(sk-[A-Za-z0-9_\-]{20,}|sk-or-[A-Za-z0-9_\-]{20,}|ghp_[A-Za-z0-9]{30,}|github_pat_[A-Za-z0-9_]{30,}|AKIA[0-9A-Z]{16}|xox[baprs]-[A-Za-z0-9-]{10,}|AIza[0-9A-Za-z_\-]{30,})\b`),
	regexp.MustCompile(`://[^/\s:@]+:[^/\s@]+@`), // user:pass@host
}

// LooksSecret keeps credentials out of the model and off disk.
func LooksSecret(cmd string) bool {
	for _, r := range secretRe {
		if r.MatchString(cmd) {
			return true
		}
	}
	return false
}

// ShouldRecord filters noise, private (leading space) and secret commands.
func ShouldRecord(cmd string) bool {
	if cmd == "" || strings.HasPrefix(cmd, " ") {
		return false
	}
	c := strings.TrimSpace(cmd)
	if c == "" || len(c) > 2000 {
		return false
	}
	if strings.HasPrefix(c, "hads predict") || strings.HasPrefix(c, "hads record") {
		return false
	}
	return !LooksSecret(c)
}

// ---------- importers ----------

// ImportAll reads zsh, bash, fish and PowerShell histories that exist on this machine.
func ImportAll() (map[string]int, []Entry) {
	counts := map[string]int{}
	var all []Entry
	h := paths.Home()
	add := func(shell string, es []Entry) {
		if len(es) == 0 {
			return
		}
		counts[shell] += len(es)
		for i := range es {
			es[i].Sess = "import-" + shell // keep transitions inside one shell's timeline
		}
		all = append(all, es...)
	}

	zsh := os.Getenv("HISTFILE")
	if zsh == "" || !strings.Contains(zsh, "zsh") {
		zdot := os.Getenv("ZDOTDIR")
		if zdot == "" {
			zdot = h
		}
		zsh = filepath.Join(zdot, ".zsh_history")
	}
	add("zsh", importZsh(zsh))
	add("bash", importPlain(filepath.Join(h, ".bash_history"), "bash"))
	fishData := os.Getenv("XDG_DATA_HOME")
	if fishData == "" {
		fishData = filepath.Join(h, ".local", "share")
	}
	add("fish", importFish(filepath.Join(fishData, "fish", "fish_history")))

	var ps []string
	if runtime.GOOS == "windows" {
		ps = append(ps, filepath.Join(os.Getenv("APPDATA"), "Microsoft", "Windows", "PowerShell", "PSReadLine", "ConsoleHost_history.txt"))
	} else {
		ps = append(ps, filepath.Join(h, ".local", "share", "powershell", "PSReadLine", "ConsoleHost_history.txt"))
	}
	for _, p := range ps {
		add("pwsh", importPlain(p, "pwsh"))
	}

	// Imported histories have no timestamps for most lines: spread them over the past
	// so recency still means something, keeping original order.
	now := time.Now().Unix()
	n := int64(len(all))
	for i := range all {
		if all[i].T == 0 {
			all[i].T = now - (n-int64(i))*60
		}
	}
	return counts, all
}

func readLines(path string) []string {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return strings.Split(string(b), "\n")
}

// zsh "metafies" bytes >= 0x80; undo that so Arabic and other UTF-8 survives.
func unmetafy(b []byte) []byte {
	out := make([]byte, 0, len(b))
	for i := 0; i < len(b); i++ {
		if b[i] == 0x83 && i+1 < len(b) {
			i++
			out = append(out, b[i]^32)
		} else {
			out = append(out, b[i])
		}
	}
	return out
}

func importZsh(path string) []Entry {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	lines := strings.Split(string(unmetafy(raw)), "\n")
	var out []Entry
	var cur strings.Builder
	var ts int64
	flush := func() {
		c := strings.TrimSpace(cur.String())
		if ShouldRecord(c) {
			out = append(out, Entry{T: ts, Cmd: c, Shell: "zsh"})
		}
		cur.Reset()
		ts = 0
	}
	for _, ln := range lines {
		if cur.Len() == 0 {
			if strings.HasPrefix(ln, ": ") {
				// ": 1700000000:0;cmd"
				if semi := strings.IndexByte(ln, ';'); semi > 0 {
					meta := strings.SplitN(ln[2:semi], ":", 2)
					ts, _ = strconv.ParseInt(strings.TrimSpace(meta[0]), 10, 64)
					ln = ln[semi+1:]
				}
			}
		} else {
			cur.WriteByte('\n')
		}
		if strings.HasSuffix(ln, `\`) {
			cur.WriteString(strings.TrimSuffix(ln, `\`))
			continue
		}
		cur.WriteString(ln)
		flush()
	}
	return out
}

func importPlain(path, shell string) []Entry {
	var out []Entry
	var ts int64
	for _, ln := range readLines(path) {
		ln = strings.TrimRight(ln, "\r")
		if shell == "bash" && strings.HasPrefix(ln, "#") {
			if v, err := strconv.ParseInt(ln[1:], 10, 64); err == nil {
				ts = v
				continue
			}
		}
		if ShouldRecord(strings.TrimSpace(ln)) {
			out = append(out, Entry{T: ts, Cmd: strings.TrimSpace(ln), Shell: shell})
		}
		ts = 0
	}
	return out
}

func importFish(path string) []Entry {
	var out []Entry
	var cur *Entry
	for _, ln := range readLines(path) {
		switch {
		case strings.HasPrefix(ln, "- cmd: "):
			if cur != nil && ShouldRecord(cur.Cmd) {
				out = append(out, *cur)
			}
			c := strings.TrimPrefix(ln, "- cmd: ")
			c = strings.ReplaceAll(c, `\n`, "\n")
			c = strings.ReplaceAll(c, `\\`, `\`)
			cur = &Entry{Cmd: strings.TrimSpace(c), Shell: "fish"}
		case strings.HasPrefix(ln, "  when: ") && cur != nil:
			cur.T, _ = strconv.ParseInt(strings.TrimSpace(strings.TrimPrefix(ln, "  when: ")), 10, 64)
		}
	}
	if cur != nil && ShouldRecord(cur.Cmd) {
		out = append(out, *cur)
	}
	return out
}
