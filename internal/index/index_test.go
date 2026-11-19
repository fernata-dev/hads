package index

import (
	"os"
	"path/filepath"
	"testing"

	"hads/internal/config"
)

func TestScanAndSearch(t *testing.T) {
	root := t.TempDir()
	mk := func(rel string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk("Documents/My Taxes 2026.xlsx")
	mk("Documents/notes.txt")
	mk("code/app/node_modules/taxes/index.js") // excluded
	mk("code/app/src/main.go")

	cfg := config.Default()
	cfg.Roots = []string{root}
	ix := New()
	if err := ix.Scan(cfg); err != nil {
		t.Fatal(err)
	}
	res := ix.Search("taxes", SearchOpts{Cwd: root, Limit: 5})
	if len(res) != 1 || filepath.Base(res[0].Path) != "My Taxes 2026.xlsx" {
		t.Fatalf("search taxes: %+v", res)
	}
	if res := ix.Search("doc/not", SearchOpts{Limit: 5}); len(res) != 1 || filepath.Base(res[0].Path) != "notes.txt" {
		t.Fatalf("path hint search: %+v", res)
	}
	if res := ix.Search("app", SearchOpts{DirsOnly: true, Limit: 5}); len(res) == 0 || !res[0].Dir {
		t.Fatalf("dirs only: %+v", res)
	}

	snap := filepath.Join(t.TempDir(), "ix.gob")
	if err := ix.Save(snap); err != nil {
		t.Fatal(err)
	}
	ix2 := New()
	if err := ix2.Load(snap); err != nil || ix2.Len() != ix.Len() {
		t.Fatalf("reload: %v %d vs %d", err, ix2.Len(), ix.Len())
	}
}

func TestMatchScoreOrder(t *testing.T) {
	exact, prefix, word, inner, fuzzy := matchScore("report", "report"), matchScore("report-2026.pdf", "report"),
		matchScore("q3-report.pdf", "report"), matchScore("myreport.pdf", "report"), matchScore("rxexpxoxrxt", "report")
	if !(exact > prefix && prefix > word && word > inner && inner > fuzzy && fuzzy > 0) {
		t.Fatalf("order broken: %v %v %v %v %v", exact, prefix, word, inner, fuzzy)
	}
	if matchScore("abc", "zzz") != 0 {
		t.Fatal("non-match must score 0")
	}
}
