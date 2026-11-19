package shell

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScriptsRender(t *testing.T) {
	for _, sh := range []string{"zsh", "bash", "fish", "pwsh", "powershell"} {
		s, err := Script(sh)
		if err != nil {
			t.Fatalf("%s: %v", sh, err)
		}
		if strings.Contains(s, "__HADS_BIN__") {
			t.Errorf("%s: placeholder left in script", sh)
		}
	}
	if _, err := Script("tcsh"); err == nil {
		t.Error("unsupported shell should error")
	}
}

func TestPwshScriptIsASCII(t *testing.T) {
	b, _ := scripts.ReadFile("scripts/hads.ps1")
	for i, c := range b {
		if c > 127 {
			t.Fatalf("non-ASCII byte at %d: Windows PowerShell 5.1 would mangle it", i)
		}
	}
}

func TestInstallIsIdempotentAndReversible(t *testing.T) {
	rc := filepath.Join(t.TempDir(), ".zshrc")
	orig := "export EDITOR=nvim\n"
	if err := os.WriteFile(rc, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	tg := []Target{{Shell: "zsh", File: rc}}
	for i := 0; i < 3; i++ {
		if errs := Install(tg); len(errs) > 0 {
			t.Fatal(errs)
		}
	}
	b, _ := os.ReadFile(rc)
	if strings.Count(string(b), markBegin) != 1 || !strings.HasPrefix(string(b), orig) {
		t.Fatalf("block not idempotent:\n%s", b)
	}
	Uninstall(tg)
	b, _ = os.ReadFile(rc)
	if string(b) != orig {
		t.Fatalf("uninstall left %q", b)
	}
}
