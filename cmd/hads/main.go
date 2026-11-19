// hads — a tiny on-device AI daemon for your terminal.
// It predicts the whole command before you type it, knows your folders and files,
// and (optionally) turns plain Arabic or English requests into safe, reviewed commands.
package main

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"hads/internal/agent"
	"hads/internal/config"
	"hads/internal/daemon"
	"hads/internal/history"
	"hads/internal/ipc"
	"hads/internal/paths"
	"hads/internal/proto"
	"hads/internal/service"
	"hads/internal/shell"
	"hads/internal/ui"
)

var version = "dev"

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		usage()
		return
	}
	cmd, rest := args[0], args[1:]
	var err error
	switch cmd {
	case "predict":
		err = cmdPredict(rest)
	case "record":
		err = cmdRecord(rest)
	case "init":
		err = cmdInit(rest)
	case "daemon":
		err = cmdDaemon(rest)
	case "setup", "install":
		err = cmdSetup(rest)
	case "status":
		err = cmdStatus()
	case "suggest", "s":
		err = cmdSuggest(rest)
	case "find", "f":
		err = cmdFind(rest)
	case "ask", "do", "?", "؟":
		err = cmdAsk(rest)
	case "explain":
		err = agent.Explain(strings.Join(rest, " "))
	case "agent", "ai":
		err = cmdAgent(rest)
	case "import":
		err = cmdImport(true)
	case "reindex":
		_, err = ipc.Call(proto.Request{Op: "reindex"}, 2*time.Second)
		if err == nil {
			fmt.Println("  " + ui.Green("✔ ") + L("re-indexing your folders in the background", "بعيد فهرسة الفولدرات في الخلفية"))
		}
	case "start":
		daemon.SetPaused(false)
		if service.Managed() {
			err = service.Restart()
		} else if !daemon.EnsureRunning(3 * time.Second) {
			err = fmt.Errorf("daemon did not start; see %s", paths.LogFile())
		}
		if err == nil {
			fmt.Println("  " + ui.Green("✔ ") + L("daemon running", "الديمون شغّال"))
		}
	case "stop":
		daemon.SetPaused(true)
		daemon.Stop()
		fmt.Println("  " + ui.Gray("■ ") + L("daemon stopped (run `hads start` to resume)", "الديمون وقف (شغّله تاني بـ `hads start`)"))
	case "restart":
		daemon.Stop()
		daemon.SetPaused(false)
		if service.Managed() {
			err = service.Restart()
		} else {
			daemon.EnsureRunning(3 * time.Second)
		}
	case "uninstall":
		err = cmdUninstall(rest)
	case "version", "--version", "-v":
		fmt.Printf("hads %s (%s/%s)\n", version, runtime.GOOS, runtime.GOARCH)
	case "help", "--help", "-h":
		usage()
	default:
		// `hads <natural language>` is a shortcut for `hads ask …`
		if ui.IsArabic(cmd) || len(args) > 2 {
			err = cmdAsk(args)
		} else {
			fmt.Fprintf(os.Stderr, "hads: unknown command %q\n\n", cmd)
			usage()
			os.Exit(2)
		}
	}
	if err != nil {
		if err.Error() != "" && !strings.Contains(err.Error(), "agent disabled") && !strings.Contains(err.Error(), "missing key") {
			fmt.Fprintln(os.Stderr, ui.Red("hads: ")+err.Error())
		}
		os.Exit(1)
	}
}

var lang = ui.Lang("")

// L picks the English or Arabic string for the user's locale.
func L(en, ar string) string {
	if lang == "ar" {
		return ar
	}
	return en
}

func banner() {
	fmt.Println()
	fmt.Println("  " + ui.Violet("◆") + " " + ui.Bold("hads") + ui.Gray("  ·  حدس  ·  "+version))
	fmt.Println("  " + ui.Gray(L("the terminal that knows what you'll type next", "التيرمنال اللي عارف هتكتب إيه قبل ما تكتبه")))
	fmt.Println()
}

func usage() {
	banner()
	rows := [][3]string{
		{"setup", "install shell hooks, autostart, learn your history", "ركّب الهوكس والتشغيل التلقائي واتعلّم من سجلّك"},
		{"status", "daemon, model and index stats", "حالة الديمون والنموذج والفهرس"},
		{"suggest [text]", "show predictions with the reasons behind them", "اعرض التوقعات وسبب كل توقّع"},
		{"find <name>", "instant search across your folders and files", "بحث فوري في كل فولدراتك وملفاتك"},
		{"ask <request>", "natural language → reviewed commands (opt-in)", "اكتب طلبك بالعربي أو الإنجليزي ← أوامر بتتراجع وتتنفذ"},
		{"explain <cmd>", "explain what a command will do", "اشرح الأمر هيعمل إيه"},
		{"agent enable", "turn on smart mode with your OpenRouter key", "فعّل الوضع الذكي بمفتاح OpenRouter"},
		{"init <shell>", "print integration for zsh|bash|fish|pwsh", "اطبع كود الربط مع الشِل"},
		{"start | stop", "control the daemon", "شغّل أو وقّف الديمون"},
		{"uninstall", "remove hooks and autostart", "شيل الهوكس والتشغيل التلقائي"},
	}
	for _, r := range rows {
		fmt.Printf("  %s  %s\n", ui.Cyan(fmt.Sprintf("%-16s", r[0])), L(r[1], r[2]))
	}
	fmt.Println()
	fmt.Println("  " + ui.Gray(L("In your shell:  →  accept · Ctrl+Space fill · Alt+N cycle · ? <request> smart mode",
		"في الشِل:  →  اقبل التوقّع · Ctrl+Space املأ · Alt+N البدائل · ? <طلبك> الوضع الذكي")))
	fmt.Println()
}

// ---------- hot path used by shells on every keystroke ----------

func b64(s string) string {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return ""
	}
	return string(b)
}

func cmdPredict(args []string) error {
	fs := flag.NewFlagSet("predict", flag.ContinueOnError)
	sh := fs.String("shell", "", "")
	sess := fs.String("sess", "", "")
	cwd := fs.String("cwd", "", "")
	cwd64 := fs.String("cwd-b64", "", "")
	buf64 := fs.String("buffer-b64", "", "")
	limit := fs.Int("limit", 5, "")
	out64 := fs.Bool("out-b64", false, "")
	prefixOnly := fs.Bool("prefix-only", false, "")
	asJSON := fs.Bool("json", false, "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	buffer := strings.Join(fs.Args(), " ")
	if *buf64 != "" {
		buffer = b64(*buf64)
	}
	if *cwd64 != "" {
		*cwd = b64(*cwd64)
	}
	if *cwd == "" {
		*cwd, _ = os.Getwd()
	}
	resp, err := ipc.Call(proto.Request{Op: "predict", Buffer: buffer, Cwd: *cwd, Sess: *sess, Shell: shell.Normalize(*sh), Limit: *limit, Lang: lang}, 1500*time.Millisecond)
	if err == ipc.ErrNoDaemon {
		_ = daemon.Spawn()
		return nil
	}
	if err != nil {
		return nil
	}
	if *asJSON {
		return json.NewEncoder(os.Stdout).Encode(resp.Suggestions)
	}
	var b strings.Builder
	for _, s := range resp.Suggestions {
		if *prefixOnly && !strings.HasPrefix(s.Text, buffer) {
			continue
		}
		if strings.ContainsRune(s.Text, '\n') {
			continue
		}
		if *out64 {
			b.WriteString(base64.StdEncoding.EncodeToString([]byte(s.Text)))
		} else {
			b.WriteString(s.Text)
		}
		b.WriteByte('\n')
	}
	os.Stdout.WriteString(b.String())
	return nil
}

func cmdRecord(args []string) error {
	fs := flag.NewFlagSet("record", flag.ContinueOnError)
	sh := fs.String("shell", "", "")
	sess := fs.String("sess", "", "")
	cwd := fs.String("cwd", "", "")
	cwd64 := fs.String("cwd-b64", "", "")
	cmd64 := fs.String("cmd-b64", "", "")
	exit := fs.Int("exit", 0, "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cmd := strings.Join(fs.Args(), " ")
	if *cmd64 != "" {
		cmd = b64(*cmd64)
	}
	if *cwd64 != "" {
		*cwd = b64(*cwd64)
	}
	if !history.ShouldRecord(cmd) {
		return nil
	}
	req := proto.Request{Op: "record", Cmd: cmd, Cwd: *cwd, Exit: *exit, Shell: shell.Normalize(*sh), Sess: *sess, Time: time.Now().Unix()}
	if _, err := ipc.Call(req, 1500*time.Millisecond); err == ipc.ErrNoDaemon {
		// never lose a command: write it straight to disk; the daemon replays it on start
		_ = history.AppendDirect(history.Entry{T: req.Time, Cmd: strings.TrimSpace(cmd), Cwd: *cwd, Exit: *exit, Shell: req.Shell, Sess: *sess})
		_ = daemon.Spawn()
	}
	return nil
}

func cmdInit(args []string) error {
	sh := shell.Current()
	if len(args) > 0 {
		sh = args[0]
	}
	s, err := shell.Script(sh)
	if err != nil {
		return err
	}
	if !ipc.Alive() {
		_ = daemon.Spawn()
	}
	fmt.Print(s)
	return nil
}

func cmdDaemon(args []string) error {
	fg := false
	for _, a := range args {
		if a == "--foreground" || a == "-f" {
			fg = true
		}
	}
	if fg {
		return daemon.Run()
	}
	daemon.SetPaused(false)
	if daemon.EnsureRunning(3 * time.Second) {
		fmt.Println("  " + ui.Green("✔ ") + L("daemon running", "الديمون شغّال"))
		return nil
	}
	return fmt.Errorf("daemon did not start; see %s", paths.LogFile())
}

// ---------- human-facing commands ----------

func cmdSuggest(args []string) error {
	buffer := strings.Join(args, " ")
	cwd, _ := os.Getwd()
	if !daemon.EnsureRunning(3 * time.Second) {
		return ipc.ErrNoDaemon
	}
	resp, err := ipc.Call(proto.Request{Op: "predict", Buffer: buffer, Cwd: cwd, Sess: "cli", Shell: shell.Current(), Limit: 8, Lang: lang}, 3*time.Second)
	if err != nil {
		return err
	}
	fmt.Println()
	if buffer == "" {
		fmt.Println("  " + ui.Violet("◆ ") + ui.Bold(L("What you'll probably run next", "غالبًا هتشغّل ده بعد كده")) + ui.Gray("  "+paths.Abbrev(cwd)))
	} else {
		fmt.Println("  " + ui.Violet("◆ ") + ui.Bold(L("Completing", "بكمّل")) + "  " + ui.Cyan(buffer))
	}
	fmt.Println("  " + ui.Rule())
	if len(resp.Suggestions) == 0 {
		fmt.Println("  " + ui.Gray(L("No prediction yet — Hads learns as you use your terminal.", "لسه مفيش توقّع — حدس بيتعلّم وانت بتستخدم التيرمنال.")))
	}
	max := 0.0
	for _, s := range resp.Suggestions {
		if s.Score > max {
			max = s.Score
		}
	}
	for i, s := range resp.Suggestions {
		bar := int(10 * s.Score / max)
		if bar < 1 {
			bar = 1
		}
		meter := ui.Violet(strings.Repeat("▰", bar)) + ui.Gray(strings.Repeat("▱", 10-bar))
		fmt.Printf("  %s %s  %s\n", ui.Gray(fmt.Sprintf("%d", i+1)), meter, ui.Bold(s.Text))
		fmt.Printf("    %s %s\n", ui.Gray("↳"), ui.Gray(fmt.Sprintf("%s · %s", s.Kind, s.Reason)))
	}
	fmt.Println()
	return nil
}

func human(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1fG", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1fM", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1fK", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%dB", n)
}

func cmdFind(args []string) error {
	fs := flag.NewFlagSet("find", flag.ContinueOnError)
	dirs := fs.Bool("dirs", false, "")
	limit := fs.Int("limit", 15, "")
	plain := fs.Bool("plain", false, "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	q := strings.Join(fs.Args(), " ")
	if q == "" {
		return fmt.Errorf("usage: hads find <name>")
	}
	if !daemon.EnsureRunning(3 * time.Second) {
		return ipc.ErrNoDaemon
	}
	cwd, _ := os.Getwd()
	t0 := time.Now()
	resp, err := ipc.Call(proto.Request{Op: "find", Query: q, Cwd: cwd, Limit: *limit, Dirs: *dirs}, 5*time.Second)
	if err != nil {
		return err
	}
	if *plain {
		for _, m := range resp.Matches {
			fmt.Println(m.Path)
		}
		return nil
	}
	fmt.Println()
	for _, m := range resp.Matches {
		icon, size := ui.Cyan("▤"), ui.Gray(fmt.Sprintf("%7s", human(m.Size)))
		if m.Dir {
			icon, size = ui.Violet("▣"), ui.Gray(fmt.Sprintf("%7s", "dir"))
		}
		dir, base := filepath.Split(paths.Abbrev(m.Path))
		fmt.Printf("  %s %s  %s%s  %s\n", icon, size, ui.Gray(dir), ui.Bold(base), ui.Gray(time.Unix(m.Mod, 0).Format("2006-01-02")))
	}
	if len(resp.Matches) == 0 {
		fmt.Println("  " + ui.Gray(L("nothing found", "مالقيتش حاجة")))
	}
	fmt.Printf("\n  %s\n\n", ui.Gray(fmt.Sprintf("%d %s · %s", len(resp.Matches), L("results", "نتيجة"), time.Since(t0).Round(time.Microsecond*100))))
	return nil
}

func cmdAsk(args []string) error {
	fs := flag.NewFlagSet("ask", flag.ContinueOnError)
	yes := fs.Bool("yes", false, "")
	dry := fs.Bool("dry-run", false, "")
	model := fs.String("model", "", "")
	b64req := fs.String("b64", "", "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	req := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if *b64req != "" {
		req = b64(*b64req)
	}
	if req == "" {
		req = ui.Ask("  " + ui.Violet("◆ ") + L("What should I do? ", "عايزني أعمل إيه؟ "))
	}
	if req == "" {
		return nil
	}
	daemon.EnsureRunning(2 * time.Second) // for file lookups
	return agent.Run(req, agent.Options{Yes: *yes, DryRun: *dry, Shell: shell.Current(), Model: *model})
}

func cmdStatus() error {
	banner()
	row := func(k, v string) { fmt.Printf("  %s  %s\n", ui.Gray(fmt.Sprintf("%-14s", k)), v) }
	resp, err := ipc.Call(proto.Request{Op: "stats"}, 2*time.Second)
	if err != nil {
		state := ui.Red("● " + L("not running", "مش شغّال"))
		if daemon.Paused() {
			state = ui.Yellow("■ " + L("stopped (hads start)", "متوقف (hads start)"))
		}
		row(L("daemon", "الديمون"), state)
	} else {
		st := resp.Stats
		up := time.Duration(toInt(st["uptime_seconds"])) * time.Second
		row(L("daemon", "الديمون"), ui.Green("● "+L("running", "شغّال"))+ui.Gray(fmt.Sprintf("  pid %d · up %s", toInt(st["pid"]), up)))
		row(L("learned", "اتعلّم"), fmt.Sprintf("%d %s · %d %s · %d %s",
			toInt(st["commands_learned"]), L("commands", "أمر"), toInt(st["unique_commands"]), L("unique", "مختلف"), toInt(st["folders_known"]), L("folders", "فولدر")))
		idx := fmt.Sprintf("%d %s", toInt(st["indexed_entries"]), L("files & folders", "ملف وفولدر"))
		if b, _ := st["indexing"].(bool); b {
			idx += ui.Yellow("  · " + L("indexing…", "بيفهرس…"))
		} else if s, ok := st["index_scan_seconds"].(float64); ok && s > 0 {
			idx += ui.Gray(fmt.Sprintf("  · %s %.1fs", L("scanned in", "اتفهرس في"), s))
		}
		row(L("index", "الفهرس"), idx)
	}
	cfg, _ := config.Load()
	if cfg.Agent.Enabled && cfg.Key() != "" {
		row(L("smart mode", "الوضع الذكي"), ui.Green("● "+L("on", "مفعّل"))+ui.Gray("  "+cfg.Agent.Model))
	} else {
		row(L("smart mode", "الوضع الذكي"), ui.Gray("○ "+L("off  (hads agent enable)", "مقفول  (hads agent enable)")))
	}
	row(L("data", "البيانات"), ui.Gray(paths.Abbrev(paths.DataDir())))
	fmt.Println()
	return nil
}

func toInt(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case int:
		return x
	}
	return 0
}

func cmdImport(force bool) error {
	if _, err := os.Stat(paths.ImportMark()); err == nil && !force {
		return nil
	}
	counts, entries := history.ImportAll()
	wasRunning := ipc.Alive()
	if wasRunning {
		daemon.Stop()
	}
	st := history.NewStore(paths.HistoryFile())
	for _, e := range entries {
		_ = st.Append(e)
	}
	_ = os.WriteFile(paths.ImportMark(), []byte(time.Now().Format(time.RFC3339)), 0o600)
	var parts []string
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s %d", k, counts[k]))
	}
	if len(parts) == 0 {
		parts = []string{L("no existing history found", "مفيش سجل قديم")}
	}
	fmt.Printf("  %s %s  %s\n", ui.Green("✔"), fmt.Sprintf("%-14s", L("learned", "اتعلّم")), fmt.Sprintf("%d %s (%s)", len(entries), L("commands", "أمر"), strings.Join(parts, ", ")))
	if wasRunning {
		daemon.EnsureRunning(3 * time.Second)
	}
	return nil
}

func cmdSetup(args []string) error {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	yes := fs.Bool("yes", false, "")
	noService := fs.Bool("no-service", false, "")
	noShell := fs.Bool("no-shell", false, "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	banner()
	ok := func(k, v string) { fmt.Printf("  %s %s  %s\n", ui.Green("✔"), fmt.Sprintf("%-14s", k), v) }
	warn := func(k, v string) { fmt.Printf("  %s %s  %s\n", ui.Yellow("!"), fmt.Sprintf("%-14s", k), v) }

	if err := paths.Ensure(); err != nil {
		return err
	}
	cfg, _ := config.Load()
	if _, err := os.Stat(paths.ConfigFile()); os.IsNotExist(err) {
		_ = cfg.Save()
	}
	ok(L("data", "البيانات"), paths.Abbrev(paths.DataDir()))

	daemon.Stop()
	_ = cmdImport(false)

	if !*noShell {
		ts := shell.Detect()
		errs := shell.Install(ts)
		var names []string
		for _, t := range ts {
			names = append(names, fmt.Sprintf("%s %s", t.Shell, ui.Gray("("+paths.Abbrev(t.File)+")")))
		}
		if len(ts) == 0 {
			warn(L("shell hooks", "ربط الشِل"), L("no supported shell found", "مالقيتش شِل مدعوم"))
		} else {
			ok(L("shell hooks", "ربط الشِل"), strings.Join(names, ", "))
		}
		for _, e := range errs {
			warn("", e.Error())
		}
	}
	daemon.SetPaused(false)
	if !*noService {
		if how, err := service.Install(); err != nil {
			warn(L("autostart", "تشغيل تلقائي"), err.Error())
		} else {
			ok(L("autostart", "تشغيل تلقائي"), how)
		}
	}
	if daemon.EnsureRunning(4 * time.Second) {
		ok(L("daemon", "الديمون"), L("running · indexing your folders in the background", "شغّال · بيفهرس فولدراتك في الخلفية"))
	} else {
		warn(L("daemon", "الديمون"), L("could not start, see ", "ماشتغلش، شوف ")+paths.Abbrev(paths.LogFile()))
	}

	if !*yes && !cfg.Agent.Enabled && ui.Interactive() {
		fmt.Println()
		fmt.Println("  " + ui.Violet("✦ ") + ui.Bold(L("Smart mode (optional)", "الوضع الذكي (اختياري)")))
		fmt.Println("    " + ui.Gray(L("Type requests in Arabic or English — e.g. “update the system and install Claude Code” —",
			"اكتب طلبك بالعربي أو الإنجليزي — زي «حدّث النظام ونزّل Claude Code وسطّبه» —")))
		fmt.Println("    " + ui.Gray(L("Hads plans the commands via OpenRouter, shows them, and runs only after you approve.",
			"وحدس يجهّز الأوامر عن طريق OpenRouter، يعرضها عليك، وماينفذش غير بموافقتك.")))
		a := strings.ToLower(ui.Ask("    " + L("Enable it now? [y/N] ", "تفعّله دلوقتي؟ [y/N] ")))
		if a == "y" || a == "yes" || a == "اه" || a == "ايوه" || a == "نعم" {
			_ = cmdAgent([]string{"enable"})
		}
	}
	fmt.Println()
	fmt.Println("  " + ui.Bold(L("Done. Open a new terminal and start typing.", "خلاص. افتح تيرمنال جديد وابدأ اكتب.")))
	fmt.Println("  " + ui.Gray(L("→ accept · Ctrl+Space fill · Alt+N cycle · hads suggest · hads find <name>",
		"→ اقبل · Ctrl+Space املأ · Alt+N البدائل · hads suggest · hads find <اسم>")))
	fmt.Println()
	return nil
}

func cmdAgent(args []string) error {
	sub := "status"
	if len(args) > 0 {
		sub = args[0]
	}
	cfg, _ := config.Load()
	switch sub {
	case "enable", "on":
		fmt.Println()
		fmt.Println("  " + ui.Violet("✦ ") + ui.Bold(L("Smart mode · OpenRouter", "الوضع الذكي · OpenRouter")))
		fmt.Println("    " + ui.Gray(L("Get a key at https://openrouter.ai/keys — it is stored only in ", "هات مفتاح من https://openrouter.ai/keys — بيتخزن بس في ")+paths.Abbrev(paths.ConfigFile())+" (0600)"))
		key := cfg.Key()
		if key != "" {
			fmt.Println("    " + ui.Gray(L("A key is already configured; press Enter to keep it.", "فيه مفتاح متسجّل؛ دوس Enter عشان تسيبه.")))
		}
		if k := ui.AskSecret("    " + L("OpenRouter key: ", "مفتاح OpenRouter: ")); k != "" {
			cfg.Agent.APIKey = k
		} else if key == "" {
			return fmt.Errorf("%s", L("no key entered", "مادخلتش مفتاح"))
		}
		if m := ui.Ask("    " + L("Model ", "الموديل ") + ui.Gray("["+cfg.Agent.Model+"]") + ": "); m != "" {
			cfg.Agent.Model = m
		}
		cfg.Agent.Enabled = true
		if err := cfg.Save(); err != nil {
			return err
		}
		sp := ui.Spin(L("checking the key…", "بتأكد من المفتاح…"))
		err := agent.Ping(cfg)
		sp.Stop()
		if err != nil {
			fmt.Println("    " + ui.Yellow("! ") + L("saved, but the test call failed: ", "اتحفظ، بس التجربة فشلت: ") + err.Error())
		} else {
			fmt.Println("    " + ui.Green("✔ ") + L("smart mode is on", "الوضع الذكي اتفعّل"))
		}
		fmt.Println("    " + ui.Gray(L("Try:", "جرّب:")) + "  " + ui.Cyan(`? update the system and install claude code`))
		fmt.Println("    " + ui.Gray(L("or: ", "أو: ")) + "  " + ui.Cyan(`hads ask "حدّث النظام ونزّل كلود كود وسطّبه"`))
		fmt.Println()
	case "disable", "off":
		cfg.Agent.Enabled = false
		if err := cfg.Save(); err != nil {
			return err
		}
		fmt.Println("  " + ui.Gray("○ ") + L("smart mode is off", "الوضع الذكي اتقفل"))
	case "model":
		if len(args) < 2 {
			fmt.Println(cfg.Agent.Model)
			return nil
		}
		cfg.Agent.Model = args[1]
		if err := cfg.Save(); err != nil {
			return err
		}
		fmt.Println("  " + ui.Green("✔ ") + L("model: ", "الموديل: ") + args[1])
	case "forget-key":
		cfg.Agent.APIKey = ""
		if err := cfg.Save(); err != nil {
			return err
		}
		fmt.Println("  " + ui.Gray("○ ") + L("key removed", "المفتاح اتمسح"))
	case "test":
		sp := ui.Spin(L("calling OpenRouter…", "بكلّم OpenRouter…"))
		err := agent.Ping(cfg)
		sp.Stop()
		if err != nil {
			return err
		}
		fmt.Println("  " + ui.Green("✔ ") + cfg.Agent.Model + " " + L("answers", "شغّال"))
	default:
		state := ui.Gray("○ off")
		if cfg.Agent.Enabled {
			state = ui.Green("● on")
		}
		keyState := ui.Red(L("missing", "مش موجود"))
		if cfg.Key() != "" {
			keyState = ui.Green(L("set", "موجود"))
		}
		fmt.Printf("\n  %s  %s\n  %s  %s\n  %s  %s\n\n", ui.Gray(L("smart mode", "الوضع الذكي")), state, ui.Gray(L("model     ", "الموديل   ")), cfg.Agent.Model, ui.Gray(L("key       ", "المفتاح   ")), keyState)
	}
	return nil
}

func cmdUninstall(args []string) error {
	purge := false
	for _, a := range args {
		if a == "--purge" {
			purge = true
		}
	}
	daemon.SetPaused(true)
	daemon.Stop()
	service.Uninstall()
	shell.Uninstall(shell.Detect())
	fmt.Println("  " + ui.Green("✔ ") + L("removed shell hooks and autostart", "اتشالت الهوكس والتشغيل التلقائي"))
	if purge {
		_ = os.RemoveAll(paths.DataDir())
		fmt.Println("  " + ui.Green("✔ ") + L("deleted ", "اتمسح ") + paths.Abbrev(paths.DataDir()))
	}
	fmt.Println("  " + ui.Gray(L("To remove the binary: ", "عشان تشيل البرنامج نفسه: ")) + paths.Exe())
	return nil
}
