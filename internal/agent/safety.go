package agent

import (
	"regexp"
	"strings"
)

// Hard blocks: these never run, whatever the model says.
var blocked = []*regexp.Regexp{
	regexp.MustCompile(`\brm\s+(-[a-zA-Z]*\s+)*-[a-zA-Z]*[rR][a-zA-Z]*\s+(-[a-zA-Z]+\s+)*(/|/\*|~|~/|~/\*|\$HOME/?|/home/?|/usr/?|/etc/?|/boot/?|/var/?|/bin/?|/lib/?)(\s|;|&|$)`),
	regexp.MustCompile(`\bmkfs(\.\w+)?\b`),
	regexp.MustCompile(`\bdd\b.*\bof=/dev/(sd|nvme|hd|vd|disk|mmcblk|xvd)`),
	regexp.MustCompile(`>\s*/dev/(sd|nvme|hd|vd|disk|mmcblk)`),
	regexp.MustCompile(`:\(\)\s*\{\s*:\s*\|\s*:\s*&\s*\}\s*;\s*:`),
	regexp.MustCompile(`\bchmod\s+(-[a-zA-Z]+\s+)*0?777\s+/(\s|$)`),
	regexp.MustCompile(`\bchown\s+-R\s+\S+\s+/(\s|$)`),
	regexp.MustCompile(`(?i)\bFormat-Volume\b|\bdiskpart\b|\bClear-Disk\b|\bformat\s+[a-z]:`),
	regexp.MustCompile(`(?i)Remove-Item\s+.*-Recurse.*\s(C:\\?|\$env:SystemRoot|\$env:USERPROFILE)(\s|$)`),
	regexp.MustCompile(`(?i)\breg\s+delete\s+HKLM`),
	regexp.MustCompile(`\bwipefs\b|\bshred\s+.*?/dev/`),
}

var risky = []struct {
	re  *regexp.Regexp
	why [2]string
}{
	{regexp.MustCompile(`\brm\s+.*-[a-zA-Z]*[rRf]`), [2]string{"deletes files recursively", "بيمسح ملفات بشكل متكرر"}},
	{regexp.MustCompile(`(?i)Remove-Item\b.*-Recurse`), [2]string{"deletes files recursively", "بيمسح ملفات بشكل متكرر"}},
	{regexp.MustCompile(`\bdd\b`), [2]string{"raw disk write", "كتابة مباشرة على الديسك"}},
	{regexp.MustCompile(`\b(shutdown|reboot|poweroff|halt)\b|Restart-Computer|Stop-Computer`), [2]string{"restarts or powers off the machine", "بيعمل ريستارت أو يطفي الجهاز"}},
	{regexp.MustCompile(`\bgit\s+(push\s+.*(--force|-f\b)|reset\s+--hard|clean\s+-[a-z]*f)`), [2]string{"rewrites or discards git history/files", "بيمسح أو يعيد كتابة شغل git"}},
	{regexp.MustCompile(`\b(apt(-get)?|dnf|yum|zypper)\s+(remove|purge|autoremove)|\bpacman\s+-R|\bbrew\s+uninstall|\bwinget\s+uninstall`), [2]string{"removes packages", "بيشيل برامج"}},
	{regexp.MustCompile(`(?i)\b(drop\s+(database|table)|truncate\s+table)\b`), [2]string{"destroys database data", "بيمسح داتا من قاعدة البيانات"}},
	{regexp.MustCompile(`\b(userdel|passwd|chpasswd|visudo)\b`), [2]string{"changes users or passwords", "بيغيّر يوزرز أو باسوردات"}},
	{regexp.MustCompile(`\b(ufw\s+disable|setenforce\s+0|systemctl\s+(stop|disable)\s+(firewalld|apparmor|ufw))\b|Set-MpPreference`), [2]string{"weakens system security", "بيضعّف حماية النظام"}},
}

var pipeToShell = regexp.MustCompile(`(curl|wget|iwr|irm|Invoke-WebRequest|Invoke-RestMethod)\b[^|]*\|\s*(sudo\s+)?(ba|z|da)?sh\b|\|\s*iex\b`)

var trustedInstallers = []string{"claude.ai/install", "sh.rustup.rs", "raw.githubusercontent.com/Homebrew/", "bun.sh/install", "astral.sh/uv", "get.docker.com", "deb.nodesource.com", "rpm.nodesource.com", "raw.githubusercontent.com/nvm-sh/", "get.pnpm.io", "deno.land/install", "ollama.com/install"}

type Assessment struct {
	Level   string // low | medium | high | blocked
	Reasons []string
}

func rank(l string) int {
	switch l {
	case "medium":
		return 1
	case "high":
		return 2
	case "blocked":
		return 3
	}
	return 0
}

// Assess combines the model's own risk label with local, model-independent checks.
func Assess(cmd, modelRisk, lang string) Assessment {
	li := 0
	if lang == "ar" {
		li = 1
	}
	a := Assessment{Level: "low"}
	raise := func(l, why string) {
		if rank(l) > rank(a.Level) {
			a.Level = l
		}
		if why != "" {
			a.Reasons = append(a.Reasons, why)
		}
	}
	if modelRisk == "medium" || modelRisk == "high" {
		raise(modelRisk, "")
	}
	for _, re := range blocked {
		if re.MatchString(cmd) {
			raise("blocked", [2]string{"matches a destructive pattern Hads never runs", "نمط مدمّر حدس عمره ما بينفذه"}[li])
			return a
		}
	}
	for _, r := range risky {
		if r.re.MatchString(cmd) {
			raise("high", r.why[li])
		}
	}
	if pipeToShell.MatchString(cmd) {
		trusted := false
		for _, t := range trustedInstallers {
			if strings.Contains(cmd, t) {
				trusted = true
			}
		}
		if trusted {
			raise("medium", [2]string{"runs an official remote installer", "بيشغّل مثبّت رسمي من النت"}[li])
		} else {
			raise("high", [2]string{"pipes a script from the internet into a shell", "بيشغّل سكربت من النت مباشرة"}[li])
		}
	}
	if strings.Contains(cmd, "sudo ") {
		raise("medium", "")
	}
	return a
}
