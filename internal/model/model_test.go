package model

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"hads/internal/config"
	"hads/internal/history"
	"hads/internal/index"
)

func feed(m *Model, sess, cwd string, cmds ...string) {
	t := time.Now().Unix() - int64(len(cmds))
	for i, c := range cmds {
		m.Record(history.Entry{T: t + int64(i), Cmd: c, Cwd: cwd, Sess: sess})
	}
}

func texts(m *Model, r Req, ix *index.Index) []string {
	var out []string
	for _, s := range m.Predict(r, ix) {
		out = append(out, s.Text)
	}
	return out
}

func TestNextCommandLearnsSequences(t *testing.T) {
	dir := t.TempDir()
	m := New()
	for i := 0; i < 3; i++ {
		feed(m, "s1", dir, "git add -A", `git commit -m "x"`, "git push", "ls")
	}
	feed(m, "s1", dir, "git add -A", `git commit -m "y"`)
	got := texts(m, Req{Sess: "s1", Cwd: dir, Limit: 3}, nil)
	if len(got) == 0 || got[0] != "git push" {
		t.Fatalf("after commit want git push first, got %q", got)
	}
}

func TestPrefixCompletion(t *testing.T) {
	dir := t.TempDir()
	m := New()
	feed(m, "s1", dir, "docker compose up -d", "docker compose logs -f", "docker compose up -d")
	got := texts(m, Req{Buffer: "docker compose u", Sess: "s2", Cwd: dir}, nil)
	if len(got) == 0 || got[0] != "docker compose up -d" {
		t.Fatalf("prefix completion got %q", got)
	}
}

func TestTypoFixAfterCommandNotFound(t *testing.T) {
	dir := t.TempDir()
	m := New()
	feed(m, "s1", dir, "git status", "git status", "git log")
	m.Record(history.Entry{T: time.Now().Unix(), Cmd: "gti status", Cwd: dir, Sess: "s1", Exit: 127})
	got := texts(m, Req{Sess: "s1", Cwd: dir, Limit: 2}, nil)
	if len(got) == 0 || got[0] != "git status" {
		t.Fatalf("want typo fix git status, got %q", got)
	}
	got = texts(m, Req{Buffer: "gti lo", Sess: "s9", Cwd: dir, Limit: 2}, nil)
	if len(got) == 0 || got[0] != "git log" {
		t.Fatalf("want git log for typed typo, got %q", got)
	}
}

func TestNeverSuggestsExit(t *testing.T) {
	dir := t.TempDir()
	m := New()
	for i := 0; i < 5; i++ {
		feed(m, "s1", dir, "ls", "exit")
	}
	feed(m, "s1", dir, "ls")
	for _, s := range texts(m, Req{Sess: "s1", Cwd: dir, Limit: 5}, nil) {
		if s == "exit" {
			t.Fatal("exit must never be predicted as the next command")
		}
	}
}

func TestCopyPredictsSourceAndDestination(t *testing.T) {
	root := t.TempDir()
	docs := filepath.Join(root, "Documents")
	dest := filepath.Join(root, "Desktop")
	for _, d := range []string{docs, dest} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(docs, "report-2026.pdf"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	ix := index.New()
	cfg := config.Default()
	cfg.Roots = []string{root}
	if err := ix.Scan(cfg); err != nil {
		t.Fatal(err)
	}
	m := New()
	feed(m, "s1", root, "cp Documents/a.txt "+dest+"/", "cp Documents/b.txt "+dest+"/")
	got := texts(m, Req{Buffer: "cp rep", Sess: "s1", Cwd: root, Shell: testShell()}, ix)
	src := "cp " + filepath.Join("Documents", "report-2026.pdf") + " "
	if len(got) == 0 || !strings.HasPrefix(got[0], src) || !strings.HasSuffix(got[0], "Desktop"+string(filepath.Separator)) {
		t.Fatalf("want full copy prediction, got %q", got)
	}
}

func TestProjectRules(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"scripts":{"dev":"vite"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New()
	got := texts(m, Req{Sess: "new", Cwd: dir, Limit: 3}, nil)
	if len(got) < 2 || got[0] != "npm install" || got[1] != "npm run dev" {
		t.Fatalf("node project rules got %q", got)
	}
}

func TestCdJumpsToFrecentFolder(t *testing.T) {
	root := t.TempDir()
	web := filepath.Join(root, "code", "webapp")
	if err := os.MkdirAll(web, 0o755); err != nil {
		t.Fatal(err)
	}
	m := New()
	feed(m, "s1", root, "cd code/webapp", "cd code/webapp")
	got := texts(m, Req{Buffer: "cd web", Sess: "s1", Cwd: root, Shell: testShell()}, nil)
	if len(got) == 0 || got[0] != "cd "+filepath.Join("code", "webapp") {
		t.Fatalf("cd jump got %q", got)
	}
}

func BenchmarkPredict(b *testing.B) {
	dir := b.TempDir()
	m := New()
	for i := 0; i < 2000; i++ {
		feed(m, "s1", dir, "git status", "git add -A", `git commit -m "w"`, "git push", "make test", "ls -la")
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Predict(Req{Buffer: "git c", Sess: "s1", Cwd: dir}, nil)
	}
}

func testShell() string {
	if runtime.GOOS == "windows" {
		return "pwsh"
	}
	return "zsh"
}
