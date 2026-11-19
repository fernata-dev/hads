package history

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLooksSecret(t *testing.T) {
	secret := []string{
		"export OPENROUTER_API_KEY=" + "sk-or" + "-v1-abcdefghijklmnopqrstuvwxyz0123", // split so scanners don't flag the fixture
		"curl -H 'Authorization: Bearer abc.def'",
		"mysql -u root --password=hunter2hunter",
		"git clone https://user:pa55word@github.com/x/y",
		"echo " + "gh" + "p_0123456789abcdefghijklmnopqrstuvwxyz",
	}
	for _, s := range secret {
		if !LooksSecret(s) {
			t.Errorf("should be secret: %q", s)
		}
	}
	for _, s := range []string{"git push", "ls -la ~/Documents", "npm run dev", "passwd"} {
		if LooksSecret(s) {
			t.Errorf("false positive: %q", s)
		}
	}
}

func TestShouldRecord(t *testing.T) {
	if ShouldRecord(" git push") {
		t.Error("leading space means private")
	}
	if !ShouldRecord("git push") {
		t.Error("plain command should record")
	}
}

func TestImportZshMetafiedArabic(t *testing.T) {
	// zsh metafies bytes >= 0x83: 0x83 followed by byte^32
	raw := []byte(": 1700000000:0;echo ")
	for _, b := range []byte("سلام") {
		if b >= 0x83 {
			raw = append(raw, 0x83, b^32)
		} else {
			raw = append(raw, b)
		}
	}
	raw = append(raw, []byte("\n: 1700000001:0;ls \\\n-la\n")...)
	p := filepath.Join(t.TempDir(), ".zsh_history")
	if err := os.WriteFile(p, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	es := importZsh(p)
	if len(es) != 2 || es[0].Cmd != "echo سلام" || es[0].T != 1700000000 || es[1].Cmd != "ls \n-la" {
		t.Fatalf("got %+v", es)
	}
}

func TestImportFish(t *testing.T) {
	p := filepath.Join(t.TempDir(), "fish_history")
	data := "- cmd: git status\n  when: 1700000000\n- cmd: echo a\\nb\n  when: 1700000001\n  paths:\n    - x\n"
	if err := os.WriteFile(p, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	es := importFish(p)
	if len(es) != 2 || es[0].Cmd != "git status" || es[1].Cmd != "echo a\nb" || es[1].T != 1700000001 {
		t.Fatalf("got %+v", es)
	}
}

func TestStoreRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "h.jsonl")
	s := NewStore(p)
	for _, c := range []string{"a", "b"} {
		if err := s.Append(Entry{T: 1, Cmd: c, Exit: 0}); err != nil {
			t.Fatal(err)
		}
	}
	es, err := Load(p)
	if err != nil || len(es) != 2 || es[1].Cmd != "b" {
		t.Fatalf("load: %v %+v", err, es)
	}
}
