// Package service registers the daemon to start at login on Linux (systemd --user),
// macOS (launchd) and Windows (Startup folder, hidden window).
package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"hads/internal/paths"
)

const (
	unitName  = "hads.service"
	plistName = "sh.hads.daemon.plist"
	label     = "sh.hads.daemon"
)

func systemdUnit() string {
	conf := os.Getenv("XDG_CONFIG_HOME")
	if conf == "" {
		conf = filepath.Join(paths.Home(), ".config")
	}
	return filepath.Join(conf, "systemd", "user", unitName)
}

func launchdPlist() string {
	return filepath.Join(paths.Home(), "Library", "LaunchAgents", plistName)
}

func startupVBS() string {
	return filepath.Join(os.Getenv("APPDATA"), "Microsoft", "Windows", "Start Menu", "Programs", "Startup", "hads.vbs")
}

func run(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %v %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func xmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
}

// Install registers autostart and returns a human description of what it did.
func Install() (string, error) {
	bin := paths.Exe()
	switch runtime.GOOS {
	case "linux":
		if _, err := exec.LookPath("systemctl"); err != nil {
			return "shell hook autostart (no systemd)", nil
		}
		unit := fmt.Sprintf(`[Unit]
Description=Hads predictive shell daemon
Documentation=https://hads.sh

[Service]
Type=simple
ExecStart="%s" daemon --foreground
Restart=on-failure
RestartSec=3
Nice=10
IOSchedulingClass=idle

[Install]
WantedBy=default.target
`, bin)
		if err := os.MkdirAll(filepath.Dir(systemdUnit()), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(systemdUnit(), []byte(unit), 0o644); err != nil {
			return "", err
		}
		_ = run("systemctl", "--user", "daemon-reload")
		if err := run("systemctl", "--user", "enable", "--now", unitName); err != nil {
			return "shell hook autostart (systemd --user unavailable)", nil
		}
		return "systemd --user (" + paths.Abbrev(systemdUnit()) + ")", nil
	case "darwin":
		plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>%s</string>
  <key>ProgramArguments</key>
  <array><string>%s</string><string>daemon</string><string>--foreground</string></array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><dict><key>SuccessfulExit</key><false/></dict>
  <key>ProcessType</key><string>Background</string>
  <key>LowPriorityIO</key><true/>
  <key>Nice</key><integer>10</integer>
  <key>StandardErrorPath</key><string>%s</string>
</dict>
</plist>
`, label, xmlEscape(bin), xmlEscape(paths.LogFile()))
		if err := os.MkdirAll(filepath.Dir(launchdPlist()), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(launchdPlist(), []byte(plist), 0o644); err != nil {
			return "", err
		}
		uid := strconv.Itoa(os.Getuid())
		_ = run("launchctl", "bootout", "gui/"+uid+"/"+label)
		if err := run("launchctl", "bootstrap", "gui/"+uid, launchdPlist()); err != nil {
			_ = run("launchctl", "load", "-w", launchdPlist())
		}
		return "launchd (" + paths.Abbrev(launchdPlist()) + ")", nil
	case "windows":
		vbs := fmt.Sprintf("CreateObject(\"WScript.Shell\").Run \"\"\"%s\"\" daemon --foreground\", 0, False\r\n", bin)
		if err := os.MkdirAll(filepath.Dir(startupVBS()), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(startupVBS(), []byte(vbs), 0o644); err != nil {
			return "", err
		}
		return "Startup folder (hidden window)", nil
	}
	return "shell hook autostart", nil
}

func Uninstall() {
	switch runtime.GOOS {
	case "linux":
		_ = run("systemctl", "--user", "disable", "--now", unitName)
		_ = os.Remove(systemdUnit())
		_ = run("systemctl", "--user", "daemon-reload")
	case "darwin":
		_ = run("launchctl", "bootout", "gui/"+strconv.Itoa(os.Getuid())+"/"+label)
		_ = os.Remove(launchdPlist())
	case "windows":
		_ = os.Remove(startupVBS())
	}
}

// Managed reports whether an OS supervisor owns the daemon (so `hads stop` should go through it).
func Managed() bool {
	switch runtime.GOOS {
	case "linux":
		return exec.Command("systemctl", "--user", "is-active", "--quiet", unitName).Run() == nil
	case "darwin":
		_, err := os.Stat(launchdPlist())
		return err == nil
	}
	return false
}

func Restart() error {
	switch runtime.GOOS {
	case "linux":
		return run("systemctl", "--user", "restart", unitName)
	case "darwin":
		return run("launchctl", "kickstart", "-k", "gui/"+strconv.Itoa(os.Getuid())+"/"+label)
	}
	return fmt.Errorf("not managed")
}
