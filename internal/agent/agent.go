// Package agent is Hads' optional natural-language mode: "update the OS and install
// Claude Code" (in Arabic or English) → a reviewed, safe, step-by-step plan that runs here.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"hads/internal/config"
	"hads/internal/ipc"
	"hads/internal/proto"
	"hads/internal/sysinfo"
	"hads/internal/ui"
)

type Step struct {
	Title   string `json:"title"`
	Command string `json:"command"`
	Explain string `json:"explain"`
	Risk    string `json:"risk"`
	Sudo    bool   `json:"sudo"`
	assess  Assessment
}

type Plan struct {
	Summary string   `json:"summary"`
	Lookup  []string `json:"lookup"`
	Steps   []Step   `json:"steps"`
	Notes   string   `json:"notes"`
}

type Options struct {
	Yes    bool
	DryRun bool
	Shell  string
	Model  string
}

var tr = map[string][2]string{
	"title":       {"Hads · smart mode", "حدس · الوضع الذكي"},
	"request":     {"Request", "الطلب"},
	"machine":     {"Machine", "الجهاز"},
	"model":       {"Model", "النموذج"},
	"thinking":    {"understanding your request…", "بفهم طلبك…"},
	"searching":   {"searching your files…", "بدوّر في ملفاتك…"},
	"planning":    {"planning the commands…", "بجهّز الأوامر…"},
	"fixing":      {"step failed — working out a fix…", "الخطوة فشلت — بفكّر في حل…"},
	"plan":        {"Plan", "الخطة"},
	"notes":       {"Notes", "ملاحظات"},
	"nosteps":     {"Nothing to run.", "مفيش أوامر تتنفذ."},
	"confirm":     {"Run it?  [Enter/y] all · [s] step by step · [n] cancel  › ", "أنفّذ؟  [Enter/y] الكل · [s] خطوة بخطوة · [n] إلغاء  › "},
	"confirmHi":   {"This plan has HIGH-RISK steps. Type 'yes' to run, 's' for step by step, anything else cancels  › ", "الخطة فيها خطوات خطيرة. اكتب 'yes' للتنفيذ، 's' خطوة بخطوة، أي حاجة تانية إلغاء  › "},
	"stepAsk":     {"run this step? [y] yes · [n] skip · [q] quit  › ", "أنفّذ الخطوة دي؟ [y] أيوه · [n] تخطّي · [q] خروج  › "},
	"cancelled":   {"Cancelled. Nothing was changed.", "اتلغى. ماحصلش أي تغيير."},
	"done":        {"done", "تمام"},
	"failed":      {"failed", "فشلت"},
	"skipped":     {"skipped", "اتخطّت"},
	"blocked":     {"blocked by the safety guard — not run", "اتمنعت بواسطة الحماية — ماتنفذتش"},
	"allDone":     {"All steps finished", "كل الخطوات خلصت"},
	"partial":     {"Finished with problems", "خلصت بس فيه مشاكل"},
	"dry":         {"Dry run — nothing executed.", "تجربة بس — ماتنفذش حاجة."},
	"fixPlan":     {"Proposed fix", "الحل المقترح"},
	"noFix":       {"No automatic fix. Output above should help.", "مفيش حل تلقائي. الناتج اللي فوق هيساعدك."},
	"sudoFirst":   {"some steps need sudo — authenticate once:", "فيه خطوات محتاجة sudo — دخّل الباسورد مرة واحدة:"},
	"disabled":    {"Smart mode is off. Turn it on with:  hads agent enable", "الوضع الذكي مقفول. فعّله بـ:  hads agent enable"},
	"nokey":       {"No OpenRouter key. Run  hads agent enable  or set OPENROUTER_API_KEY", "مفيش مفتاح OpenRouter. شغّل  hads agent enable  أو حط OPENROUTER_API_KEY"},
	"risk_low":    {"low", "آمن"},
	"risk_medium": {"medium", "متوسط"},
	"risk_high":   {"high", "خطير"},
}

func t(lang, k string) string {
	if v, ok := tr[k]; ok {
		if lang == "ar" {
			return v[1]
		}
		return v[0]
	}
	return k
}

const systemPrompt = `You are Hads — an expert, careful system operator that lives inside a command-line tool on the user's own computer.
The user writes a request in natural language: Arabic (often Egyptian dialect), English, or a mix. Turn it into an exact, ordered plan of shell commands for THIS machine. Hads shows the plan to the user and only runs it after they approve.

MACHINE (JSON):
%s

HOW TO PLAN
- Steps run one by one through %s, non-interactively (stdin is not available except for the sudo password). On Windows write PowerShell.
- Use only package managers listed in the machine facts; if one is truly needed and missing, install it first as its own step.
- Every command must be non-interactive: -y / --noconfirm / --needed / DEBIAN_FRONTEND=noninteractive / --accept-package-agreements --accept-source-agreements, etc.
- Use sudo only when required, only if has_sudo is true and is_root is false. Never sudo for Homebrew, npm user installs, or AUR helpers (paru/yay).
- One logical action per step so failures are easy to locate. Use && inside a step only for tightly coupled actions.
- Prefer official packages and official installers. Known official installers:
  • Claude Code — macOS/Linux/WSL: curl -fsSL https://claude.ai/install.sh | bash   • Windows PowerShell: irm https://claude.ai/install.ps1 | iex   • alternative: npm install -g @anthropic-ai/claude-code (needs Node.js 18+)
  • Homebrew — NONINTERACTIVE=1 /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
  • Rust — curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y     • uv — curl -LsSf https://astral.sh/uv/install.sh | sh     • Bun — curl -fsSL https://bun.sh/install | bash
- "Update the system/OS" = the OS package manager's full upgrade (pacman -Syu --noconfirm, apt update && apt full-upgrade -y, dnf upgrade -y --refresh, zypper -n dup, brew update && brew upgrade, winget upgrade --all --silent --accept-package-agreements --accept-source-agreements, etc.).
- For tasks on the user's files use real absolute paths from FILES results or user_dirs. If you must locate files first, return zero steps and put 1–3 short filename keywords in "lookup"; Hads searches its index of the disk and sends results back.
- Add a final verification step when it makes sense (for example: claude --version).
- Never do destructive things (deleting data, wiping disks, changing users/passwords, disabling security) unless the user explicitly asked for exactly that.
- If the request is unclear, unsafe, or not a computer task, return zero steps and explain briefly in "notes".

LANGUAGE
Write "summary", every "title" and "explain", and "notes" in the user's language: Egyptian Arabic if they wrote Arabic, English if English. Commands stay commands.

OUTPUT
Reply with exactly ONE JSON object and nothing else:
{"summary": string, "lookup": [string], "steps": [{"title": string, "command": string, "explain": string, "risk": "low"|"medium"|"high", "sudo": boolean}], "notes": string}`

func parsePlan(s string) (Plan, error) {
	var p Plan
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	i, j := strings.Index(s, "{"), strings.LastIndex(s, "}")
	if i < 0 || j <= i {
		return p, errors.New("model did not return a plan")
	}
	err := json.Unmarshal([]byte(s[i:j+1]), &p)
	return p, err
}

type session struct {
	cfg    *config.Config
	client *Client
	info   sysinfo.Info
	lang   string
	msgs   []Msg
	opts   Options
}

func (s *session) ask(ctx context.Context, user string, spin string) (Plan, error) {
	s.msgs = append(s.msgs, Msg{Role: "user", Content: user})
	sp := ui.Spin(spin)
	out, err := s.client.Chat(ctx, s.msgs, true, 2500)
	sp.Stop()
	if err != nil {
		return Plan{}, err
	}
	s.msgs = append(s.msgs, Msg{Role: "assistant", Content: out})
	p, err := parsePlan(out)
	if err != nil {
		// one retry asking for strict JSON
		s.msgs = append(s.msgs, Msg{Role: "user", Content: "That was not valid JSON. Reply again with ONLY the JSON object."})
		sp = ui.Spin(spin)
		out, err = s.client.Chat(ctx, s.msgs, true, 2500)
		sp.Stop()
		if err != nil {
			return Plan{}, err
		}
		s.msgs = append(s.msgs, Msg{Role: "assistant", Content: out})
		p, err = parsePlan(out)
	}
	return p, err
}

func lookupFiles(keys []string, cwd string) string {
	var b strings.Builder
	for _, k := range keys {
		if len(keys) > 3 {
			break
		}
		resp, err := ipc.Call(proto.Request{Op: "find", Query: k, Cwd: cwd, Limit: 8}, 3*time.Second)
		fmt.Fprintf(&b, "FILES matching %q:\n", k)
		if err != nil || len(resp.Matches) == 0 {
			b.WriteString("  (none found)\n")
			continue
		}
		for _, m := range resp.Matches {
			kind := "file"
			if m.Dir {
				kind = "dir"
			}
			fmt.Fprintf(&b, "  - %s (%s, modified %s)\n", m.Path, kind, time.Unix(m.Mod, 0).Format("2006-01-02"))
		}
	}
	return b.String()
}

func machineLine(in sysinfo.Info) string {
	d := in.Distro
	if d == "" {
		d = in.OS
	}
	return fmt.Sprintf("%s · %s · %s · %s", d, in.Arch, in.Shell, strings.Join(in.PackageManagers, ", "))
}

// Run is `hads ask "<request>"`.
func Run(request string, opts Options) error {
	cfg, _ := config.Load()
	lang := "en"
	if ui.IsArabic(request) {
		lang = "ar"
	}
	if !cfg.Agent.Enabled {
		fmt.Fprintln(os.Stderr, ui.Yellow("  "+t(lang, "disabled")))
		return errors.New("agent disabled")
	}
	key := cfg.Key()
	if key == "" {
		fmt.Fprintln(os.Stderr, ui.Yellow("  "+t(lang, "nokey")))
		return errors.New("missing key")
	}
	mdl := cfg.Agent.Model
	if opts.Model != "" {
		mdl = opts.Model
	}
	info := sysinfo.Collect(opts.Shell)
	if resp, err := ipc.Call(proto.Request{Op: "stats"}, time.Second); err == nil {
		if td, ok := resp.Stats["top_dirs"].([]any); ok && len(td) > 0 {
			var dirs []string
			for _, d := range td {
				dirs = append(dirs, fmt.Sprint(d))
			}
			info.Tools["home_folders"] = strings.Join(dirs, ", ")
		}
	}
	infoJSON, _ := json.MarshalIndent(info, "", "  ")
	execSh, _ := sysinfo.ExecShell()

	s := &session{cfg: cfg, client: NewClient(cfg.Agent.BaseURL, key, mdl), info: info, lang: lang, opts: opts}
	s.msgs = []Msg{{Role: "system", Content: fmt.Sprintf(systemPrompt, infoJSON, filepath.Base(execSh))}}

	fmt.Println()
	fmt.Println("  " + ui.Violet("◆ ") + ui.Bold(t(lang, "title")))
	fmt.Println("  " + ui.Rule())
	fmt.Printf("  %s  %s\n", ui.Gray(pad(t(lang, "request"), 8)), ui.Bold(request))
	fmt.Printf("  %s  %s\n", ui.Gray(pad(t(lang, "machine"), 8)), machineLine(info))
	fmt.Printf("  %s  %s\n\n", ui.Gray(pad(t(lang, "model"), 8)), ui.Cyan(mdl))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	plan, err := s.ask(ctx, request, t(lang, "thinking"))
	for round := 0; err == nil && len(plan.Steps) == 0 && len(plan.Lookup) > 0 && round < 2; round++ {
		sp := ui.Spin(t(lang, "searching"))
		found := lookupFiles(plan.Lookup, info.Cwd)
		sp.Stop()
		plan, err = s.ask(ctx, found+"\nNow return the full plan.", t(lang, "planning"))
	}
	if err != nil {
		return explainErr(err, lang)
	}
	return s.execute(ctx, plan, 0)
}

func explainErr(err error, lang string) error {
	var he *HTTPError
	if errors.As(err, &he) {
		hint := ""
		switch he.Status {
		case 401:
			hint = [2]string{"invalid OpenRouter key — run: hads agent enable", "مفتاح OpenRouter غلط — شغّل: hads agent enable"}[b2i(lang == "ar")]
		case 402:
			hint = [2]string{"not enough OpenRouter credits", "رصيد OpenRouter مش كفاية"}[b2i(lang == "ar")]
		case 404:
			hint = [2]string{"model not found — try: hads agent model anthropic/claude-sonnet-5.5", "الموديل مش موجود — جرّب: hads agent model anthropic/claude-sonnet-5.5"}[b2i(lang == "ar")]
		case 429:
			hint = [2]string{"rate limited — wait a moment and retry", "عدد الطلبات كتير — استنى شوية وجرّب تاني"}[b2i(lang == "ar")]
		}
		if hint != "" {
			fmt.Fprintln(os.Stderr, "  "+ui.Red("✘ ")+hint)
		}
	}
	fmt.Fprintln(os.Stderr, "  "+ui.Red("✘ ")+err.Error())
	return err
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func pad(s string, n int) string {
	if r := []rune(s); len(r) < n {
		return s + strings.Repeat(" ", n-len(r))
	}
	return s
}

func (s *session) showPlan(p *Plan, heading string) (hasHigh, hasBlocked bool) {
	L := s.lang
	if p.Summary != "" {
		fmt.Printf("  %s  %s\n\n", ui.Violet("✦"), ui.Bold(p.Summary))
	} else {
		fmt.Printf("  %s\n\n", ui.Bold(heading))
	}
	for i := range p.Steps {
		st := &p.Steps[i]
		st.assess = Assess(st.Command, strings.ToLower(st.Risk), L)
		tag := ""
		switch st.assess.Level {
		case "low":
			tag = ui.Green(t(L, "risk_low"))
		case "medium":
			tag = ui.Yellow(t(L, "risk_medium"))
		case "high":
			tag = ui.Red(t(L, "risk_high"))
			hasHigh = true
		case "blocked":
			tag = ui.Red("⛔")
			hasBlocked = true
		}
		if st.Sudo || strings.Contains(st.Command, "sudo ") {
			tag = ui.Gray("sudo · ") + tag
		}
		fmt.Printf("  %s  %s  %s\n", ui.Cyan(fmt.Sprintf("%2d", i+1)), ui.Bold(st.Title), tag)
		for _, ln := range strings.Split(st.Command, "\n") {
			fmt.Printf("      %s %s\n", ui.Violet("$"), ln)
		}
		if st.Explain != "" {
			fmt.Printf("      %s\n", ui.Gray(st.Explain))
		}
		for _, r := range st.assess.Reasons {
			fmt.Printf("      %s %s\n", ui.Yellow("!"), ui.Yellow(r))
		}
		fmt.Println()
	}
	if p.Notes != "" {
		fmt.Printf("  %s %s\n\n", ui.Gray(t(L, "notes")+":"), p.Notes)
	}
	return
}

// execute shows, confirms and runs a plan; on failure it asks the model for a fix.
func (s *session) execute(ctx context.Context, p Plan, attempt int) error {
	L := s.lang
	hasHigh, _ := s.showPlan(&p, t(L, "plan"))
	if len(p.Steps) == 0 {
		fmt.Println("  " + ui.Gray(t(L, "nosteps")))
		return nil
	}
	if s.opts.DryRun {
		fmt.Println("  " + ui.Gray(t(L, "dry")))
		return nil
	}
	stepMode := false
	if !(s.opts.Yes && !hasHigh) {
		q := t(L, "confirm")
		if hasHigh {
			q = t(L, "confirmHi")
		}
		ans := strings.ToLower(ui.Ask("  " + ui.Bold(q)))
		switch {
		case ans == "s" || ans == "س":
			stepMode = true
		case hasHigh && (ans == "yes" || ans == "نعم" || ans == "ايوه" || ans == "أيوه"):
		case !hasHigh && (ans == "" || ans == "y" || ans == "yes" || ans == "ا" || ans == "اه" || ans == "ايوه" || ans == "نعم"):
		default:
			fmt.Println("  " + ui.Gray(t(L, "cancelled")))
			return nil
		}
	}

	// authenticate sudo once, up front, with a real terminal
	for _, st := range p.Steps {
		if st.assess.Level != "blocked" && strings.Contains(st.Command, "sudo ") {
			if _, err := exec.LookPath("sudo"); err == nil {
				fmt.Println("  " + ui.Gray(t(L, "sudoFirst")))
				c := exec.Command("sudo", "-v")
				c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
				_ = c.Run()
			}
			break
		}
	}

	start := time.Now()
	okCount := 0
	for i, st := range p.Steps {
		fmt.Printf("\n  %s %s  %s\n", ui.Violet("▶"), ui.Cyan(fmt.Sprintf("%d/%d", i+1, len(p.Steps))), ui.Bold(st.Title))
		if st.assess.Level == "blocked" {
			fmt.Println("    " + ui.Red("⛔ "+t(L, "blocked")))
			continue
		}
		fmt.Printf("    %s %s\n", ui.Violet("$"), ui.Gray(st.Command))
		if stepMode {
			a := strings.ToLower(ui.Ask("    " + t(L, "stepAsk")))
			if a == "q" {
				fmt.Println("  " + ui.Gray(t(L, "cancelled")))
				return nil
			}
			if a != "" && a != "y" && a != "yes" && a != "اه" && a != "ايوه" {
				fmt.Println("    " + ui.Gray("↷ "+t(L, "skipped")))
				continue
			}
		}
		t0 := time.Now()
		code, tail := runStep(ctx, st.Command)
		dur := time.Since(t0).Round(100 * time.Millisecond)
		if code == 0 {
			okCount++
			fmt.Printf("    %s %s %s\n", ui.Green("✔"), ui.Green(t(L, "done")), ui.Gray("("+dur.String()+")"))
			continue
		}
		fmt.Printf("    %s %s %s\n", ui.Red("✘"), ui.Red(fmt.Sprintf("%s (exit %d)", t(L, "failed"), code)), ui.Gray("("+dur.String()+")"))
		if !s.cfg.Agent.AutoFix || attempt >= s.cfg.Agent.MaxFixAttempts {
			fmt.Println("  " + ui.Yellow(t(L, "noFix")))
			return fmt.Errorf("step %d failed", i+1)
		}
		var rest []string
		for _, r := range p.Steps[i+1:] {
			rest = append(rest, r.Command)
		}
		restJSON, _ := json.Marshal(rest)
		msg := fmt.Sprintf("Step %d %q failed.\nCommand: %s\nExit code: %d\nLast output:\n```\n%s\n```\nSteps not run yet: %s\nReturn a corrected plan (same JSON format) with the steps to run NOW to recover and finish the original task (replace the failed step and everything after it). If it cannot be fixed automatically, return zero steps and explain in \"notes\".",
			i+1, st.Title, st.Command, code, tail, restJSON)
		fix, err := s.ask(ctx, msg, t(L, "fixing"))
		if err != nil {
			return explainErr(err, L)
		}
		fmt.Println()
		fmt.Println("  " + ui.Rule())
		fmt.Println("  " + ui.Yellow("↻ ") + ui.Bold(t(L, "fixPlan")))
		fmt.Println()
		return s.execute(ctx, fix, attempt+1)
	}
	fmt.Println()
	fmt.Println("  " + ui.Rule())
	label := t(L, "allDone")
	mark := ui.Green("✔")
	if okCount < len(p.Steps) {
		label, mark = t(L, "partial"), ui.Yellow("●")
	}
	fmt.Printf("  %s %s  %s\n\n", mark, ui.Bold(label), ui.Gray(fmt.Sprintf("%d/%d · %s", okCount, len(p.Steps), time.Since(start).Round(time.Second))))
	return nil
}

// tailBuf keeps the last N bytes of output for the repair prompt.
type tailBuf struct {
	mu  sync.Mutex
	b   []byte
	max int
}

func (t *tailBuf) Write(p []byte) (int, error) {
	t.mu.Lock()
	t.b = append(t.b, p...)
	if len(t.b) > t.max {
		t.b = t.b[len(t.b)-t.max:]
	}
	t.mu.Unlock()
	return len(p), nil
}

func runStep(ctx context.Context, command string) (int, string) {
	sh, args := sysinfo.ExecShell()
	cmd := exec.CommandContext(ctx, sh, append(args, command)...)
	tb := &tailBuf{max: 6000}
	cmd.Stdin = os.Stdin
	cmd.Stdout = io.MultiWriter(os.Stdout, tb)
	cmd.Stderr = io.MultiWriter(os.Stderr, tb)
	cmd.Dir, _ = os.Getwd()
	cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive", "HADS_AGENT=1")
	err := cmd.Run()
	code := 0
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		} else {
			code = 127
			fmt.Fprintln(tb, err.Error())
		}
	}
	return code, string(tb.b)
}

// Explain asks the model what a command does, in the user's language.
func Explain(command string) error {
	cfg, _ := config.Load()
	lang := ui.Lang("")
	if !cfg.Agent.Enabled || cfg.Key() == "" {
		fmt.Fprintln(os.Stderr, ui.Yellow("  "+t(lang, "disabled")))
		return errors.New("agent disabled")
	}
	a := Assess(command, "", lang)
	c := NewClient(cfg.Agent.BaseURL, cfg.Key(), cfg.Agent.Model)
	language := "English"
	if lang == "ar" {
		language = "Egyptian Arabic"
	}
	sp := ui.Spin(t(lang, "thinking"))
	out, err := c.Chat(context.Background(), []Msg{
		{Role: "system", Content: "Explain shell commands briefly for the user's OS (" + sysinfo.Collect("").Distro + "). Use " + language + ". Plain text, max 8 short lines: what it does, each important flag, and any risk."},
		{Role: "user", Content: command},
	}, false, 600)
	sp.Stop()
	if err != nil {
		return explainErr(err, lang)
	}
	fmt.Println()
	fmt.Printf("  %s %s\n\n", ui.Violet("$"), ui.Bold(command))
	for _, ln := range strings.Split(strings.TrimSpace(out), "\n") {
		fmt.Println("  " + ln)
	}
	if a.Level != "low" {
		fmt.Printf("\n  %s %s %s\n", ui.Yellow("!"), ui.Yellow(a.Level), ui.Gray(strings.Join(a.Reasons, " · ")))
	}
	fmt.Println()
	return nil
}

// Ping checks the key and model with a tiny request.
func Ping(cfg *config.Config) error {
	c := NewClient(cfg.Agent.BaseURL, cfg.Key(), cfg.Agent.Model)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	_, err := c.Chat(ctx, []Msg{{Role: "user", Content: "Reply with the single word: ok"}}, false, 8)
	return err
}
