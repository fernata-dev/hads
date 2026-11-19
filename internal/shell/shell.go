// Package shell renders the per-shell integration and wires it into rc files.
package shell

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"hads/internal/paths"
)

//go:embed scripts/*
var scripts embed.FS

var Supported = []string{"zsh", "bash", "fish", "pwsh"}

func quotePosix(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
func quotePwsh(s string) string  { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
func quoteFish(s string) string {
	return "'" + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), "'", `\'`) + "'"
}

// Normalize maps aliases (powershell → pwsh).
func Normalize(sh string) string {
	sh = strings.ToLower(strings.TrimSuffix(filepath.Base(sh), ".exe"))
	switch sh {
	case "powershell", "pwsh", "ps":
		return "pwsh"
	}
	return sh
}

// Script returns the integration code for `eval "$(hads init zsh)"` and friends.
func Script(sh string) (string, error) {
	sh = Normalize(sh)
	file := map[string]string{"zsh": "hads.zsh", "bash": "hads.bash", "fish": "hads.fish", "pwsh": "hads.ps1"}[sh]
	if file == "" {
		return "", fmt.Errorf("unsupported shell %q (supported: %s)", sh, strings.Join(Supported, ", "))
	}
	b, err := scripts.ReadFile("scripts/" + file)
	if err != nil {
		return "", err
	}
	bin := paths.Exe()
	var q string
	switch sh {
	case "pwsh":
		q = quotePwsh(bin)
	case "fish":
		q = quoteFish(bin)
	default:
		q = quotePosix(bin)
	}
	return strings.ReplaceAll(string(b), "__HADS_BIN__", q), nil
}

const (
	markBegin = "# >>> hads >>>"
	markEnd   = "# <<< hads <<<"
)

// Target is one rc file we manage.
type Target struct {
	Shell string
	File  string
}

func zshrc() string {
	if z := os.Getenv("ZDOTDIR"); z != "" {
		return filepath.Join(z, ".zshrc")
	}
	return filepath.Join(paths.Home(), ".zshrc")
}

func pwshProfiles() []string {
	var out []string
	for _, exe := range []string{"pwsh", "powershell"} {
		if _, err := exec.LookPath(exe); err != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		b, err := exec.CommandContext(ctx, exe, "-NoProfile", "-NonInteractive", "-Command", "$PROFILE.CurrentUserAllHosts").Output()
		cancel()
		if err == nil {
			if p := strings.TrimSpace(string(b)); p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

func has(name string) bool { _, err := exec.LookPath(name); return err == nil }

// Detect lists rc files for shells installed on this machine.
func Detect() []Target {
	h := paths.Home()
	var t []Target
	if has("zsh") || fileExists(zshrc()) {
		t = append(t, Target{"zsh", zshrc()})
	}
	if runtime.GOOS != "windows" && (has("bash") || fileExists(filepath.Join(h, ".bashrc"))) {
		t = append(t, Target{"bash", filepath.Join(h, ".bashrc")})
		if runtime.GOOS == "darwin" {
			t = append(t, Target{"bash", filepath.Join(h, ".bash_profile")})
		}
	}
	if has("fish") {
		conf := os.Getenv("XDG_CONFIG_HOME")
		if conf == "" {
			conf = filepath.Join(h, ".config")
		}
		t = append(t, Target{"fish", filepath.Join(conf, "fish", "conf.d", "hads.fish")})
	}
	for _, p := range pwshProfiles() {
		t = append(t, Target{"pwsh", p})
	}
	return t
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

func block(t Target) string {
	bin := paths.Exe()
	binDir := filepath.Dir(bin)
	switch t.Shell {
	case "fish":
		return fmt.Sprintf("%s\nfish_add_path -g %s\n%s init fish | source\n%s\n", markBegin, quoteFish(binDir), quoteFish(bin), markEnd)
	case "pwsh":
		return fmt.Sprintf("%s\nif ($env:PATH -notlike %s) { $env:PATH = %s + [IO.Path]::PathSeparator + $env:PATH }\n& %s init pwsh | Out-String | Invoke-Expression\n%s\n",
			markBegin, quotePwsh("*"+binDir+"*"), quotePwsh(binDir), quotePwsh(bin), markEnd)
	default:
		return fmt.Sprintf("%s\ncase \":$PATH:\" in *\":%s:\"*) ;; *) export PATH=%s\":$PATH\" ;; esac\neval \"$(%s init %s)\"\n%s\n",
			markBegin, binDir, quotePosix(binDir), quotePosix(bin), t.Shell, markEnd)
	}
}

func stripBlock(s string) string {
	for {
		i := strings.Index(s, markBegin)
		if i < 0 {
			return s
		}
		j := strings.Index(s[i:], markEnd)
		if j < 0 {
			return s
		}
		end := i + j + len(markEnd)
		if end < len(s) && s[end] == '\n' {
			end++
		}
		s = s[:i] + s[end:]
	}
}

// Install writes (or refreshes) the hads block in each rc file.
func Install(ts []Target) []error {
	var errs []error
	for _, t := range ts {
		if err := os.MkdirAll(filepath.Dir(t.File), 0o755); err != nil {
			errs = append(errs, err)
			continue
		}
		old, _ := os.ReadFile(t.File)
		body := stripBlock(string(old))
		if t.Shell == "fish" { // our own conf.d file
			body = ""
		} else if body != "" && !strings.HasSuffix(body, "\n") {
			body += "\n"
		}
		if t.Shell != "fish" && len(old) > 0 && !fileExists(t.File+".hads-backup") {
			_ = os.WriteFile(t.File+".hads-backup", old, 0o600)
		}
		body += "\n" + block(t)
		if err := os.WriteFile(t.File, []byte(strings.TrimLeft(body, "\n")), 0o644); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", t.File, err))
		}
	}
	return errs
}

// Uninstall removes our block (and our fish conf.d file).
func Uninstall(ts []Target) {
	for _, t := range ts {
		if t.Shell == "fish" {
			_ = os.Remove(t.File)
			continue
		}
		old, err := os.ReadFile(t.File)
		if err != nil {
			continue
		}
		if s := stripBlock(string(old)); s != string(old) {
			_ = os.WriteFile(t.File, []byte(strings.TrimRight(s, "\n")+"\n"), 0o644)
		}
	}
}

// Current guesses the shell that launched us.
func Current() string {
	if runtime.GOOS == "windows" {
		return "pwsh"
	}
	if s := os.Getenv("SHELL"); s != "" {
		return Normalize(s)
	}
	return "bash"
}

var ErrUnsupported = errors.New("unsupported shell")
