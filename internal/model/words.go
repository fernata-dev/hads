package model

import (
	"path/filepath"
	"strings"
)

// Split tokenizes a (possibly unfinished) command line. It understands ' " and \.
// lastStart is the byte offset where the final token begins in s.
func Split(s string) (toks []string, lastStart int, endsWithSpace bool) {
	var cur strings.Builder
	inTok := false
	var q byte
	start := 0
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case q != 0:
			if ch == q {
				q = 0
			} else if ch == '\\' && q == '"' && i+1 < len(s) {
				i++
				cur.WriteByte(s[i])
			} else {
				cur.WriteByte(ch)
			}
		case ch == '\'' || ch == '"':
			if !inTok {
				inTok, start = true, i
			}
			q = ch
		case ch == '\\' && i+1 < len(s) && filepath.Separator == '/':
			if !inTok {
				inTok, start = true, i
			}
			i++
			cur.WriteByte(s[i])
		case ch == ' ' || ch == '\t' || ch == '\n':
			if inTok {
				toks = append(toks, cur.String())
				cur.Reset()
				inTok = false
			}
		default:
			if !inTok {
				inTok, start = true, i
			}
			cur.WriteByte(ch)
		}
	}
	if inTok {
		toks = append(toks, cur.String())
		lastStart = start
		return toks, lastStart, false
	}
	return toks, len(s), len(s) > 0
}

var wrappers = map[string]bool{"sudo": true, "doas": true, "time": true, "nohup": true, "command": true, "builtin": true, "exec": true, "env": true, "nice": true, "caffeinate": true}

// ProgIndex returns the index of the real program token (skipping sudo, env vars, ...).
func ProgIndex(toks []string) int {
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if wrappers[t] {
			continue
		}
		if i > 0 && wrappers[toks[i-1]] && strings.HasPrefix(t, "-") {
			continue
		}
		if strings.Contains(t, "=") && !strings.HasPrefix(t, "=") && !strings.ContainsAny(t[:strings.Index(t, "=")], "/.-") {
			continue // FOO=bar cmd
		}
		return i
	}
	return -1
}

func ProgName(t string) string {
	t = strings.ToLower(filepath.Base(t))
	return strings.TrimSuffix(t, ".exe")
}

// Program extracts the effective program name of a command.
func Program(cmd string) string {
	toks, _, _ := Split(cmd)
	i := ProgIndex(toks)
	if i < 0 {
		return ""
	}
	return ProgName(toks[i])
}

const safeChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_./:@%+=,-~"

func needsQuote(s string) bool {
	for _, r := range s {
		if r == '\\' && filepath.Separator == '\\' {
			continue // Windows path separator
		}
		if r < 128 && !strings.ContainsRune(safeChars, r) {
			return true
		}
	}
	return s == ""
}

// Quote renders a path for the given shell, keeping a leading ~ expandable.
func Quote(s, shell string) string {
	if !needsQuote(s) {
		return s
	}
	if shell == "pwsh" || shell == "powershell" {
		return "'" + strings.ReplaceAll(s, "'", "''") + "'"
	}
	prefix := ""
	if strings.HasPrefix(s, "~/") {
		prefix, s = "~/", s[2:]
		if !needsQuote(s) {
			return prefix + s
		}
	}
	if shell == "fish" {
		return prefix + "'" + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), "'", `\'`) + "'"
	}
	return prefix + "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Distance is Damerau-Levenshtein (optimal string alignment) on runes.
func Distance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	la, lb := len(ra), len(rb)
	d := make([][]int, la+1)
	for i := range d {
		d[i] = make([]int, lb+1)
		d[i][0] = i
	}
	for j := 0; j <= lb; j++ {
		d[0][j] = j
	}
	for i := 1; i <= la; i++ {
		for j := 1; j <= lb; j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			d[i][j] = min(d[i-1][j]+1, d[i][j-1]+1, d[i-1][j-1]+cost)
			if i > 1 && j > 1 && ra[i-1] == rb[j-2] && ra[i-2] == rb[j-1] {
				d[i][j] = min(d[i][j], d[i-2][j-2]+1)
			}
		}
	}
	return d[la][lb]
}

func short(s string) string {
	r := []rune(s)
	if len(r) > 40 {
		return string(r[:39]) + "…"
	}
	return s
}
