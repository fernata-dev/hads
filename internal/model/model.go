// Package model is Hads' on-device predictor: a small statistical language model
// over your own command stream (n-gram transitions + frecency + folder context +
// file awareness + rules). It learns online, runs in microseconds and never leaves the machine.
package model

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"hads/internal/history"
	"hads/internal/index"
	"hads/internal/paths"
	"hads/internal/proto"
)

const (
	cmdHalfLife = 14 * 86400.0
	dirHalfLife = 30 * 86400.0
)

type CmdStat struct {
	Count int
	Fails int
	Frec  float64
	Last  int64
}

type DirStat struct {
	Frec float64
	Last int64
}

type sessState struct {
	last     []string
	lastExit int
	lastCwd  string
	t        int64
}

type Model struct {
	mu       sync.RWMutex
	cmds     map[string]*CmdStat
	next     map[string]map[string]float64
	next2    map[string]map[string]float64
	progNext map[string]map[string]float64
	cwdCmds  map[string]map[string]float64
	dirs     map[string]*DirStat
	dests    map[string]map[string]float64
	pathUse  map[string]float64
	programs map[string]int
	sess     map[string]*sessState
	latest   *sessState
	total    int

	pathMu    sync.Mutex
	pathCache map[string]pathHit
}

type pathHit struct {
	ok bool
	t  time.Time
}

func New() *Model {
	return &Model{
		cmds:      map[string]*CmdStat{},
		next:      map[string]map[string]float64{},
		next2:     map[string]map[string]float64{},
		progNext:  map[string]map[string]float64{},
		cwdCmds:   map[string]map[string]float64{},
		dirs:      map[string]*DirStat{},
		dests:     map[string]map[string]float64{},
		pathUse:   map[string]float64{},
		programs:  map[string]int{},
		sess:      map[string]*sessState{},
		pathCache: map[string]pathHit{},
	}
}

func decayed(f float64, last, now int64, half float64) float64 {
	if now <= last {
		return f
	}
	return f * math.Exp2(-float64(now-last)/half)
}

func bump(m map[string]map[string]float64, a, b string, w float64) {
	row := m[a]
	if row == nil {
		row = map[string]float64{}
		m[a] = row
	}
	row[b] += w
}

func rowProbs(row map[string]float64) (sum float64) {
	for _, v := range row {
		sum += v
	}
	return
}

var copyLike = map[string]bool{"cp": true, "mv": true, "rsync": true, "scp": true, "copy": true, "move": true, "copy-item": true, "move-item": true, "robocopy": true, "xcopy": true, "ln": true}
var cdLike = map[string]bool{"cd": true, "pushd": true, "z": true, "set-location": true, "sl": true, "j": true}

// Record learns from one executed command.
func (m *Model) Record(e history.Entry) {
	cmd := strings.TrimSpace(e.Cmd)
	if cmd == "" {
		return
	}
	if e.T == 0 {
		e.T = time.Now().Unix()
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	sessKey := e.Sess
	s := m.sess[sessKey]
	if s == nil {
		s = &sessState{}
		m.sess[sessKey] = s
	}
	defer func() {
		s.last = append(s.last, cmd)
		if len(s.last) > 2 {
			s.last = s.last[len(s.last)-2:]
		}
		s.lastExit, s.lastCwd, s.t = e.Exit, e.Cwd, e.T
		m.latest = s
	}()

	st := m.cmds[cmd]
	if st == nil {
		st = &CmdStat{}
		m.cmds[cmd] = st
	}
	st.Frec = decayed(st.Frec, st.Last, e.T, cmdHalfLife) + 1
	st.Count++
	st.Last = e.T
	if e.Exit != 0 && e.Exit != 130 {
		st.Fails++
	}
	if e.Exit == 127 {
		return // typos teach us nothing about what comes next
	}
	m.total++

	if n := len(s.last); n > 0 {
		prev := s.last[n-1]
		bump(m.next, prev, cmd, 1)
		bump(m.progNext, Program(prev), cmd, 1)
		if n > 1 {
			bump(m.next2, s.last[n-2]+"\x00"+prev, cmd, 1)
		}
	}
	if e.Cwd != "" {
		bump(m.cwdCmds, e.Cwd, cmd, 1)
		m.bumpDir(e.Cwd, 0.25, e.T)
	}

	toks, _, _ := Split(cmd)
	pi := ProgIndex(toks)
	if pi < 0 {
		return
	}
	prog := ProgName(toks[pi])
	m.programs[prog]++
	if e.Cwd == "" || e.Exit != 0 {
		return
	}
	var args []string
	for _, t := range toks[pi+1:] {
		if !strings.HasPrefix(t, "-") {
			args = append(args, t)
		}
	}
	switch {
	case cdLike[prog] && len(args) > 0 && args[0] != "-":
		m.bumpDir(paths.Resolve(e.Cwd, args[0]), 1, e.T)
	case copyLike[prog] && len(args) >= 2:
		dest := args[len(args)-1]
		if !strings.Contains(dest, ":") || filepath.VolumeName(dest) != "" {
			abs := paths.Resolve(e.Cwd, dest)
			if strings.HasSuffix(dest, "/") || strings.HasSuffix(dest, `\`) || isDir(abs) {
				bump(m.dests, prog, abs, 1)
				bump(m.dests, "*", abs, 1)
			}
		}
	}
	for _, a := range args {
		if strings.ContainsAny(a, `/\`) || (strings.Contains(a, ".") && !strings.Contains(a, "://")) {
			m.pathUse[paths.Resolve(e.Cwd, a)] += 1
		}
	}
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

func (m *Model) bumpDir(p string, w float64, t int64) {
	d := m.dirs[p]
	if d == nil {
		d = &DirStat{}
		m.dirs[p] = d
	}
	d.Frec = decayed(d.Frec, d.Last, t, dirHalfLife) + w
	d.Last = t
}

// PathBoost is used by the file index to rank paths you actually use.
func (m *Model) PathBoost(p string) float64 {
	b := 0.0
	if v := m.pathUse[p]; v > 0 {
		b += 1.5 * math.Log1p(v)
	}
	if d := m.dirs[p]; d != nil {
		b += 1.2 * math.Log1p(d.Frec)
	}
	return b
}

func (m *Model) Stats() map[string]any {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return map[string]any{
		"commands_learned": m.total,
		"unique_commands":  len(m.cmds),
		"transitions":      len(m.next),
		"folders_known":    len(m.dirs),
		"programs":         len(m.programs),
	}
}

// ---------- prediction ----------

type Req struct {
	Buffer, Cwd, Sess, Shell, Lang string
	Limit                          int
}

type ctxInfo struct {
	last, last2 string
	lastExit    int
	lastCwd     string
	cwd         string
	shell       string
	lang        string
	now         int64
}

type collector struct {
	m    map[string]*proto.Suggestion
	best map[string]float64
}

func newCollector() *collector {
	return &collector{m: map[string]*proto.Suggestion{}, best: map[string]float64{}}
}

func (c *collector) add(text string, score float64, kind, reason string) {
	text = strings.TrimRight(text, " ")
	if text == "" || score == 0 {
		return
	}
	s := c.m[text]
	if s == nil {
		s = &proto.Suggestion{Text: text}
		c.m[text] = s
	}
	s.Score += score
	if score > c.best[text] {
		c.best[text] = score
		s.Kind, s.Reason = kind, reason
	}
}

func (c *collector) top(n int, exclude string) []proto.Suggestion {
	out := make([]proto.Suggestion, 0, len(c.m))
	for _, s := range c.m {
		if s.Text == exclude || s.Score <= 0.05 {
			continue
		}
		s.Score = math.Round(s.Score*100) / 100
		out = append(out, *s)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return out[i].Text < out[j].Text
		}
		return out[i].Score > out[j].Score
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}

func (m *Model) context(r Req) ctxInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ci := ctxInfo{cwd: r.Cwd, shell: r.Shell, lang: r.Lang, now: time.Now().Unix()}
	s := m.sess[r.Sess]
	if s == nil && (r.Sess == "" || r.Sess == "cli") {
		s = m.latest // `hads suggest` from a terminal we can't identify: use the most recent one
	}
	if s != nil {
		if n := len(s.last); n > 0 {
			ci.last = s.last[n-1]
			if n > 1 {
				ci.last2 = s.last[n-2]
			}
		}
		ci.lastExit, ci.lastCwd = s.lastExit, s.lastCwd
	}
	return ci
}

// Predict returns ranked suggestions for the current buffer (empty = "what next?").
func (m *Model) Predict(r Req, ix *index.Index) []proto.Suggestion {
	if r.Limit <= 0 {
		r.Limit = 5
	}
	ci := m.context(r)
	c := newCollector()
	buf := strings.TrimLeft(r.Buffer, " ")
	rules := m.rules(ci) // filesystem / git work happens outside the model lock

	m.mu.RLock()
	defer m.mu.RUnlock()
	if strings.TrimSpace(buf) == "" {
		m.nextCommand(ci, c)
		for _, rl := range rules {
			c.add(rl.text, rl.w, rl.kind, rl.reason)
		}
		m.penalize(ci, c)
		return c.top(r.Limit, "")
	}
	m.prefixHistory(ci, buf, c)
	for _, rl := range rules {
		if strings.HasPrefix(rl.text, buf) && rl.text != buf {
			c.add(rl.text, rl.w*0.8, rl.kind, rl.reason)
		}
	}
	m.argCompletion(ci, buf, c, ix)
	m.typoFix(ci, buf, c)
	m.penalize(ci, c)
	return c.top(r.Limit, strings.TrimRight(buf, " "))
}

func conf(sum float64) float64 { return sum / (sum + 1.5) }

func (m *Model) transitionScores(ci ctxInfo, add func(text string, s float64, reason string)) {
	if ci.last != "" {
		if row := m.next[ci.last]; row != nil {
			sum := rowProbs(row)
			for nx, w := range row {
				add(nx, 6*(w/sum)*conf(sum), T(ci.lang, "after", short(ci.last)))
			}
		}
		if ci.last2 != "" {
			if row := m.next2[ci.last2+"\x00"+ci.last]; row != nil {
				sum := rowProbs(row)
				for nx, w := range row {
					add(nx, 5*(w/sum)*conf(sum), T(ci.lang, "after2", short(ci.last)))
				}
			}
		}
		p := Program(ci.last)
		if row := m.progNext[p]; row != nil {
			sum := rowProbs(row)
			for nx, w := range row {
				add(nx, 1.6*(w/sum)*conf(sum), T(ci.lang, "afterProg", p))
			}
		}
	}
	if row := m.cwdCmds[ci.cwd]; row != nil {
		sum := rowProbs(row)
		for nx, w := range row {
			add(nx, 2.5*(w/sum)*conf(sum), T(ci.lang, "here"))
		}
	}
}

func (m *Model) nextCommand(ci ctxInfo, c *collector) {
	m.transitionScores(ci, func(t string, s float64, why string) { c.add(t, s, "next", why) })
	// weak global prior: your most frecent commands
	type kv struct {
		k string
		f float64
	}
	var top []kv
	for k, st := range m.cmds {
		top = append(top, kv{k, decayed(st.Frec, st.Last, ci.now, cmdHalfLife)})
	}
	sort.Slice(top, func(i, j int) bool { return top[i].f > top[j].f })
	for i := 0; i < len(top) && i < 25; i++ {
		c.add(top[i].k, 0.35*math.Log1p(top[i].f), "history", T(ci.lang, "popular"))
	}
}

func (m *Model) prefixHistory(ci ctxInfo, buf string, c *collector) {
	scores := map[string]float64{}
	reasons := map[string]string{}
	m.transitionScores(ci, func(t string, s float64, why string) {
		if strings.HasPrefix(t, buf) {
			scores[t] += s
			if _, ok := reasons[t]; !ok {
				reasons[t] = why
			}
		}
	})
	for text, st := range m.cmds {
		if len(text) <= len(buf) || !strings.HasPrefix(text, buf) {
			continue
		}
		if st.Count > 0 && st.Fails == st.Count {
			continue
		}
		f := 1.5 + 1.2*math.Log1p(decayed(st.Frec, st.Last, ci.now, cmdHalfLife))
		why := reasons[text]
		if why == "" {
			why = T(ci.lang, "history", st.Count)
		}
		c.add(text, f+scores[text], "history", why)
	}
}

var neverNext = map[string]bool{"exit": true, "logout": true, "reboot": true, "shutdown": true, "poweroff": true, "shutdown now": true, "sudo reboot": true, "sudo shutdown now": true, "sudo poweroff": true}

func (m *Model) penalize(ci ctxInfo, c *collector) {
	for text, s := range c.m {
		if neverNext[text] {
			s.Score = -1 // never offer to close the terminal or the machine
		}
		if st := m.cmds[text]; st != nil && st.Count > 0 {
			s.Score -= 2.5 * float64(st.Fails) / float64(st.Count)
		}
		if text == ci.last && ci.lastExit != 0 {
			s.Score -= 3
		}
	}
}

// ---------- arguments, files and folders ----------

// programs whose first argument is a subcommand, not a path
var subcmdProgs = map[string]bool{"git": true, "docker": true, "podman": true, "npm": true, "pnpm": true, "yarn": true, "bun": true, "cargo": true, "go": true, "kubectl": true, "systemctl": true, "apt": true, "apt-get": true, "dnf": true, "brew": true, "pip": true, "pip3": true, "uv": true, "poetry": true, "gh": true, "helm": true, "terraform": true, "flatpak": true, "snap": true, "winget": true, "choco": true, "scoop": true, "make": true, "npx": true, "rustup": true, "conda": true, "hads": true, "claude": true, "journalctl": true, "ip": true, "nmcli": true, "code": false}

var fileProgs = map[string]bool{"cat": true, "less": true, "more": true, "bat": true, "vim": true, "nvim": true, "vi": true, "nano": true, "code": true, "subl": true, "micro": true, "hx": true, "emacs": true, "open": true, "xdg-open": true, "start": true, "rm": true, "chmod": true, "chown": true, "tar": true, "unzip": true, "zip": true, "python": true, "python3": true, "node": true, "bash": true, "sh": true, "source": true, ".": true, "head": true, "tail": true, "wc": true, "du": true, "file": true, "stat": true, "touch": true, "ls": true, "exa": true, "eza": true, "go": true, "gcc": true, "g++": true, "docker": true, "git": true, "notepad": true, "explorer": true, "get-content": true, "remove-item": true, "invoke-item": true, "ii": true, "mpv": true, "vlc": true, "feh": true, "gimp": true, "libreoffice": true, "evince": true, "okular": true, "zathura": true, "sha256sum": true, "md5sum": true, "gpg": true, "ffmpeg": true}

func (m *Model) render(p, cwd, shell, partial string) string {
	sep := string(filepath.Separator)
	var out string
	switch {
	case strings.HasPrefix(partial, "/") || filepath.IsAbs(partial):
		out = p
	case strings.HasPrefix(partial, "~"):
		out = paths.Abbrev(p)
	case cwd != "" && strings.HasPrefix(p, cwd+sep):
		out = p[len(cwd)+1:]
		if strings.HasPrefix(partial, "./") || strings.HasPrefix(partial, `.\`) {
			out = "." + sep + out
		}
	default:
		out = paths.Abbrev(p)
	}
	return Quote(out, shell)
}

func (m *Model) bestDest(prog, avoid string) string {
	best, bw := "", 0.0
	for _, key := range []string{prog, "*"} {
		for d, w := range m.dests[key] {
			if key == "*" {
				w *= 0.6
			}
			if d != avoid && w > bw && isDir(d) {
				best, bw = d, w
			}
		}
	}
	return best
}

func (m *Model) argCompletion(ci ctxInfo, buf string, c *collector, ix *index.Index) {
	toks, lastStart, endsSpace := Split(buf)
	pi := ProgIndex(toks)
	if pi < 0 || (pi == len(toks)-1 && !endsSpace) {
		return // still typing the program name
	}
	prog := ProgName(toks[pi])
	partial := ""
	head := buf
	if !endsSpace {
		partial = toks[len(toks)-1]
		head = buf[:lastStart]
	}
	if strings.HasPrefix(partial, "-") || strings.Contains(partial, "://") {
		return
	}
	argIdx := 0
	upto := len(toks)
	if !endsSpace {
		upto--
	}
	for _, t := range toks[pi+1 : upto] {
		if !strings.HasPrefix(t, "-") {
			argIdx++
		}
	}
	sep := string(filepath.Separator)

	// cd-like: jump anywhere you've been, zoxide style
	if cdLike[prog] {
		q := strings.ToLower(strings.TrimRight(partial, `/\`))
		type dk struct {
			p string
			s float64
		}
		var ds []dk
		for p, d := range m.dirs {
			if p == ci.cwd {
				continue
			}
			lp := strings.ToLower(p)
			ms := 0.0
			base := strings.ToLower(filepath.Base(p))
			switch {
			case q == "":
				ms = 0.5
			case base == q:
				ms = 6
			case strings.HasPrefix(base, q):
				ms = 4.5
			case strings.Contains(base, q):
				ms = 3
			case strings.Contains(lp, q):
				ms = 1.5
			}
			if ms == 0 {
				continue
			}
			ds = append(ds, dk{p, ms + 1.3*math.Log1p(decayed(d.Frec, d.Last, ci.now, dirHalfLife))})
		}
		sort.Slice(ds, func(i, j int) bool { return ds[i].s > ds[j].s })
		for i := 0; i < len(ds) && i < 4; i++ {
			if !isDir(ds[i].p) {
				continue
			}
			c.add(head+m.render(ds[i].p, ci.cwd, ci.shell, partial), 3+ds[i].s*0.6, "path", T(ci.lang, "jump", paths.Abbrev(ds[i].p)))
		}
		if partial != "" && ix != nil {
			for _, mt := range ix.Search(partial, index.SearchOpts{DirsOnly: true, Cwd: ci.cwd, Limit: 4, Boost: m.PathBoost}) {
				c.add(head+m.render(mt.Path, ci.cwd, ci.shell, partial), 2.5+mt.Score*0.35, "path", T(ci.lang, "foundIn", paths.Abbrev(filepath.Dir(mt.Path))))
			}
		}
		return
	}

	// destination slot of cp/mv: suggest where you usually put things
	if copyLike[prog] && argIdx >= 1 && partial == "" {
		type dk struct {
			p string
			w float64
		}
		var ds []dk
		for _, key := range []string{prog, "*"} {
			for d, w := range m.dests[key] {
				ds = append(ds, dk{d, w})
			}
		}
		sort.Slice(ds, func(i, j int) bool { return ds[i].w > ds[j].w })
		for i := 0; i < len(ds) && i < 3; i++ {
			if isDir(ds[i].p) {
				c.add(head+m.render(ds[i].p, ci.cwd, ci.shell, "~")+sep, 4+math.Log1p(ds[i].w), "path", T(ci.lang, "dest"))
			}
		}
		return
	}

	if ix == nil || (subcmdProgs[prog] && argIdx == 0) {
		return
	}
	if partial == "" && !fileProgs[prog] && !copyLike[prog] {
		return
	}
	limit := 5
	opts := index.SearchOpts{Cwd: ci.cwd, Limit: limit, Boost: m.PathBoost}
	if partial == "" {
		opts.Limit = 3
	}
	for _, mt := range ix.Search(partial, opts) {
		if partial == "" && filepath.Dir(mt.Path) != ci.cwd {
			continue
		}
		r := m.render(mt.Path, ci.cwd, ci.shell, partial)
		if mt.Dir && !cdLike[prog] {
			r += sep
		}
		why := T(ci.lang, "foundIn", paths.Abbrev(filepath.Dir(mt.Path)))
		if filepath.Dir(mt.Path) == ci.cwd {
			why = T(ci.lang, "fileHere")
			if mt.Dir {
				why = T(ci.lang, "dirHere")
			}
			if partial == "" {
				why = T(ci.lang, "recentFile")
			}
		}
		if len([]rune(partial)) <= 2 && !strings.HasPrefix(strings.ToLower(filepath.Base(mt.Path)), strings.ToLower(partial)) {
			continue // too short to trust a fuzzy match
		}
		score := 1.8 + mt.Score*0.32
		text := head + r
		// the full prediction: source AND destination of a copy, in one go
		if copyLike[prog] && argIdx == 0 && !mt.Dir {
			if d := m.bestDest(prog, filepath.Dir(mt.Path)); d != "" {
				full := text + " " + m.render(d, ci.cwd, ci.shell, "~") + sep
				c.add(full, score+1.5, "path", T(ci.lang, "copyTo", paths.Abbrev(d)))
			}
		}
		c.add(text, score, "path", why)
	}
}

var builtins = map[string]bool{"cd": true, "echo": true, "export": true, "source": true, ".": true, "alias": true, "set": true, "unset": true, "exit": true, "history": true, "type": true, "jobs": true, "fg": true, "bg": true, "pushd": true, "popd": true, "eval": true, "exec": true, "read": true, "printf": true, "test": true, "[": true, "ulimit": true, "umask": true, "wait": true, "kill": true, "function": true, "if": true, "for": true, "while": true, "case": true, "then": true, "do": true, "abbr": true, "functions": true, "complete": true, "bind": true, "builtin": true, "command": true, "hash": true, "local": true, "return": true, "shift": true, "trap": true, "true": true, "false": true, "dirs": true, "disown": true, "let": true, "declare": true, "typeset": true, "setopt": true, "unsetopt": true, "bindkey": true, "autoload": true, "zle": true, "rehash": true, "noglob": true}

var commonProgs = []string{"git", "ls", "cd", "cat", "grep", "sudo", "docker", "npm", "pnpm", "yarn", "node", "python3", "python", "pip", "cargo", "go", "make", "vim", "nvim", "nano", "code", "ssh", "scp", "curl", "wget", "tar", "unzip", "kubectl", "systemctl", "journalctl", "pacman", "apt", "dnf", "brew", "winget", "mkdir", "rm", "cp", "mv", "touch", "chmod", "chown", "find", "less", "head", "tail", "htop", "top", "claude"}

func (m *Model) onPath(prog string) bool {
	m.pathMu.Lock()
	h, ok := m.pathCache[prog]
	m.pathMu.Unlock()
	if ok && time.Since(h.t) < 2*time.Minute {
		return h.ok
	}
	_, err := exec.LookPath(prog)
	m.pathMu.Lock()
	m.pathCache[prog] = pathHit{ok: err == nil, t: time.Now()}
	m.pathMu.Unlock()
	return err == nil
}

func (m *Model) closestProgram(p string) string {
	best, bd := "", 99
	maxD := 1
	if len([]rune(p)) > 4 {
		maxD = 2
	}
	try := func(cand string) {
		if cand == p || cand == "" {
			return
		}
		if d := Distance(p, cand); d <= maxD && d < bd {
			best, bd = cand, d
		}
	}
	for prog, n := range m.programs {
		if n >= 2 {
			try(prog)
		}
	}
	for _, cp := range commonProgs {
		try(cp)
	}
	return best
}

func (m *Model) typoFix(ci ctxInfo, buf string, c *collector) {
	toks, _, endsSpace := Split(buf)
	pi := ProgIndex(toks)
	if pi < 0 || (pi == len(toks)-1 && !endsSpace) {
		return
	}
	raw := toks[pi]
	p := ProgName(raw)
	if strings.ContainsAny(raw, `/\`) || builtins[p] || m.programs[p] > 0 || m.onPath(p) {
		return
	}
	if fix := m.closestProgram(p); fix != "" {
		i := strings.Index(buf, raw)
		fixed := buf[:i] + fix + buf[i+len(raw):]
		why := T(ci.lang, "typo", p, fix)
		c.add(fixed, 6, "fix", why)
		// and finish the corrected line from history: "gti pu" → "git push"
		best, bf := "", 0.0
		for text, st := range m.cmds {
			if len(text) > len(fixed) && strings.HasPrefix(text, fixed) && st.Fails < st.Count {
				if f := decayed(st.Frec, st.Last, ci.now, cmdHalfLife); f > bf {
					best, bf = text, f
				}
			}
		}
		if best != "" {
			c.add(best, 7.5, "fix", why)
		}
	}
}

// fixLast repairs the program name of a command that just failed with 127.
func (m *Model) fixLast(cmd string) (string, string) {
	toks, _, _ := Split(cmd)
	pi := ProgIndex(toks)
	if pi < 0 {
		return "", ""
	}
	raw := toks[pi]
	fix := m.closestProgram(ProgName(raw))
	if fix == "" {
		return "", ""
	}
	i := strings.Index(cmd, raw)
	return cmd[:i] + fix + cmd[i+len(raw):], fix
}

func (m *Model) String() string { return fmt.Sprintf("model(%d cmds)", len(m.cmds)) }
