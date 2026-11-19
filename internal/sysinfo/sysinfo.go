// Package sysinfo describes this machine so the agent plans commands that actually fit it.
package sysinfo

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"hads/internal/paths"
)

type Info struct {
	OS              string            `json:"os"`
	Distro          string            `json:"distro,omitempty"`
	DistroID        string            `json:"distro_id,omitempty"`
	DistroLike      string            `json:"distro_like,omitempty"`
	Version         string            `json:"version,omitempty"`
	Kernel          string            `json:"kernel,omitempty"`
	Arch            string            `json:"arch"`
	Shell           string            `json:"shell"`
	ExecShell       string            `json:"exec_shell"`
	User            string            `json:"user"`
	IsRoot          bool              `json:"is_root"`
	HasSudo         bool              `json:"has_sudo"`
	Home            string            `json:"home"`
	Cwd             string            `json:"cwd"`
	PackageManagers []string          `json:"package_managers"`
	Tools           map[string]string `json:"tools"`
	UserDirs        map[string]string `json:"user_dirs,omitempty"`
	WSL             bool              `json:"wsl,omitempty"`
}

func run(timeout time.Duration, name string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func has(name string) bool { _, err := exec.LookPath(name); return err == nil }

func osRelease() map[string]string {
	m := map[string]string{}
	for _, p := range []string{"/etc/os-release", "/usr/lib/os-release"} {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			k, v, ok := strings.Cut(sc.Text(), "=")
			if ok {
				m[k] = strings.Trim(v, `"'`)
			}
		}
		f.Close()
		break
	}
	return m
}

// ExecShell is the interpreter used to run planned steps.
func ExecShell() (string, []string) {
	if runtime.GOOS == "windows" {
		if p, err := exec.LookPath("pwsh"); err == nil {
			return p, []string{"-NoProfile", "-ExecutionPolicy", "Bypass", "-Command"}
		}
		return "powershell", []string{"-NoProfile", "-ExecutionPolicy", "Bypass", "-Command"}
	}
	if p, err := exec.LookPath("bash"); err == nil {
		return p, []string{"-c"}
	}
	return "/bin/sh", []string{"-c"}
}

func Collect(shell string) Info {
	in := Info{OS: runtime.GOOS, Arch: runtime.GOARCH, Shell: shell, Home: paths.Home(), Tools: map[string]string{}}
	in.Cwd, _ = os.Getwd()
	if u, err := user.Current(); err == nil {
		in.User = u.Username
	}
	sh, _ := ExecShell()
	in.ExecShell = filepath.Base(sh)

	switch runtime.GOOS {
	case "linux":
		r := osRelease()
		in.Distro, in.DistroID, in.DistroLike, in.Version = r["PRETTY_NAME"], r["ID"], r["ID_LIKE"], r["VERSION_ID"]
		in.Kernel = run(time.Second, "uname", "-r")
		if b, err := os.ReadFile("/proc/version"); err == nil && strings.Contains(strings.ToLower(string(b)), "microsoft") {
			in.WSL = true
		}
	case "darwin":
		in.Distro = "macOS " + run(time.Second, "sw_vers", "-productVersion")
		in.Kernel = run(time.Second, "uname", "-r")
	case "windows":
		in.Distro = "Windows"
		in.Version = run(2*time.Second, "cmd", "/c", "ver")
	default:
		in.Kernel = run(time.Second, "uname", "-sr")
	}
	if runtime.GOOS != "windows" {
		in.IsRoot = os.Geteuid() == 0
		in.HasSudo = has("sudo")
		if !in.HasSudo && has("doas") {
			in.Tools["doas"] = "yes"
		}
	}
	for _, pm := range []string{"apt", "dnf", "yum", "pacman", "paru", "yay", "zypper", "apk", "xbps-install", "emerge", "nix", "brew", "port", "winget", "choco", "scoop", "snap", "flatpak"} {
		if has(pm) {
			in.PackageManagers = append(in.PackageManagers, pm)
		}
	}
	for _, t := range []string{"git", "curl", "wget", "node", "npm", "pnpm", "bun", "python3", "pip", "docker", "go", "cargo", "claude", "code", "systemctl"} {
		if has(t) {
			in.Tools[t] = "installed"
		}
	}
	if has("node") {
		in.Tools["node"] = run(1500*time.Millisecond, "node", "--version")
	}
	in.UserDirs = userDirs()
	return in
}

// userDirs maps well-known folders (Desktop, Downloads, …) — localized names included.
func userDirs() map[string]string {
	h := paths.Home()
	out := map[string]string{}
	if b, err := os.ReadFile(filepath.Join(h, ".config", "user-dirs.dirs")); err == nil {
		for _, ln := range strings.Split(string(b), "\n") {
			k, v, ok := strings.Cut(ln, "=")
			if !ok || !strings.HasPrefix(k, "XDG_") {
				continue
			}
			v = strings.ReplaceAll(strings.Trim(v, `"`), "$HOME", h)
			name := strings.ToLower(strings.TrimSuffix(strings.TrimPrefix(k, "XDG_"), "_DIR"))
			out[name] = v
		}
	}
	for _, d := range []string{"Desktop", "Downloads", "Documents", "Pictures", "Music", "Videos"} {
		k := strings.ToLower(d)
		if _, ok := out[k]; ok {
			continue
		}
		if fi, err := os.Stat(filepath.Join(h, d)); err == nil && fi.IsDir() {
			out[k] = filepath.Join(h, d)
		}
	}
	return out
}
