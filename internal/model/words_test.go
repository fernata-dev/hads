package model

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestSplit(t *testing.T) {
	cases := []struct {
		in       string
		toks     []string
		endSpace bool
	}{
		{"git status", []string{"git", "status"}, false},
		{"git commit -m \"hello world\"", []string{"git", "commit", "-m", "hello world"}, false},
		{"cp 'My File.pdf' ~/Desktop/ ", []string{"cp", "My File.pdf", "~/Desktop/"}, true},
		{"", nil, false},
	}
	for _, c := range cases {
		toks, _, end := Split(c.in)
		if !reflect.DeepEqual(toks, c.toks) || end != c.endSpace {
			t.Errorf("Split(%q) = %q,%v want %q,%v", c.in, toks, end, c.toks, c.endSpace)
		}
	}
	if filepath.Separator == '/' {
		if toks, _, _ := Split(`echo a\ b`); !reflect.DeepEqual(toks, []string{"echo", "a b"}) {
			t.Errorf("backslash escape: %q", toks)
		}
	}
	_, start, _ := Split("cp rep")
	if start != 3 {
		t.Errorf("lastStart = %d, want 3", start)
	}
}

func TestProgram(t *testing.T) {
	cases := map[string]string{
		"git push":                    "git",
		"sudo pacman -Syu":            "pacman",
		"sudo -E env FOO=1 make all":  "make",
		"FOO=bar BAZ=1 ./build.sh":    "build.sh",
		"/usr/bin/python3 -m venv .v": "python3",
		"time nohup node server.js":   "node",
	}
	for in, want := range cases {
		if got := Program(in); got != want {
			t.Errorf("Program(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestQuote(t *testing.T) {
	cases := []struct{ in, shell, want string }{
		{"report.pdf", "zsh", "report.pdf"},
		{"~/Desktop", "zsh", "~/Desktop"},
		{"~/My Docs/a.pdf", "zsh", "~/'My Docs/a.pdf'"},
		{"it's.txt", "bash", `'it'\''s.txt'`},
		{"My Docs", "pwsh", "'My Docs'"},
		{"it's", "pwsh", "'it''s'"},
		{"a b", "fish", "'a b'"},
		{"تقرير.pdf", "zsh", "تقرير.pdf"},
	}
	for _, c := range cases {
		if got := Quote(c.in, c.shell); got != c.want {
			t.Errorf("Quote(%q,%s) = %q, want %q", c.in, c.shell, got, c.want)
		}
	}
}

func TestDistance(t *testing.T) {
	cases := []struct {
		a, b string
		d    int
	}{
		{"git", "git", 0}, {"gti", "git", 1}, {"dokcer", "docker", 1}, {"kubectl", "kubctl", 1}, {"npm", "pnpm", 1}, {"abc", "xyz", 3},
	}
	for _, c := range cases {
		if got := Distance(c.a, c.b); got != c.d {
			t.Errorf("Distance(%q,%q) = %d, want %d", c.a, c.b, got, c.d)
		}
	}
}
