package model

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"hads/internal/index"
	"hads/internal/proto"
)

type rule struct {
	text, kind, reason string
	w                  float64
}

// SearchFiles runs an index query ranked with this model's sense of what you use.
func (m *Model) SearchFiles(ix *index.Index, q string, o index.SearchOpts) []proto.Match {
	m.mu.RLock()
	defer m.mu.RUnlock()
	o.Boost = m.PathBoost
	return ix.Search(q, o)
}

// ---------- cached probes (git, project files) ----------

type gitInfo struct {
	repo                                       bool
	branch                                     string
	staged, unstaged, untracked, ahead, behind int
	noUpstream                                 bool
}

type cacheEntry[T any] struct {
	v T
	t time.Time
}

var (
	probeMu   sync.Mutex
	gitCache  = map[string]cacheEntry[gitInfo]{}
	projCache = map[string]cacheEntry[[]rule]{}
)

func findUp(dir, name string) string {
	for i := 0; i < 40; i++ {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

var aheadRe = regexp.MustCompile(`ahead (\d+)`)
var behindRe = regexp.MustCompile(`behind (\d+)`)

func probeGit(cwd string) gitInfo {
	root := findUp(cwd, ".git")
	if root == "" {
		return gitInfo{}
	}
	probeMu.Lock()
	if e, ok := gitCache[root]; ok && time.Since(e.t) < 3*time.Second {
		probeMu.Unlock()
		return e.v
	}
	probeMu.Unlock()
	gi := gitInfo{repo: true}
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", root, "status", "--porcelain=v1", "-b").Output()
	if err == nil {
		for i, ln := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
			if i == 0 && strings.HasPrefix(ln, "## ") {
				h := ln[3:]
				br := h
				if j := strings.Index(h, "..."); j >= 0 {
					br = h[:j]
				} else {
					gi.noUpstream = !strings.HasPrefix(h, "No commits") && !strings.Contains(h, "HEAD (no branch)")
					if j := strings.IndexByte(h, ' '); j >= 0 {
						br = h[:j]
					}
				}
				gi.branch = br
				if mm := aheadRe.FindStringSubmatch(h); mm != nil {
					gi.ahead, _ = strconv.Atoi(mm[1])
				}
				if mm := behindRe.FindStringSubmatch(h); mm != nil {
					gi.behind, _ = strconv.Atoi(mm[1])
				}
				continue
			}
			if len(ln) < 3 {
				continue
			}
			x, y := ln[0], ln[1]
			switch {
			case x == '?' && y == '?':
				gi.untracked++
			default:
				if x != ' ' {
					gi.staged++
				}
				if y != ' ' {
					gi.unstaged++
				}
			}
		}
	}
	probeMu.Lock()
	gitCache[root] = cacheEntry[gitInfo]{gi, time.Now()}
	probeMu.Unlock()
	return gi
}

func exists(dir, name string) bool {
	_, err := os.Stat(filepath.Join(dir, name))
	return err == nil
}

func (m *Model) projectRules(ci ctxInfo) []rule {
	cwd := ci.cwd
	if cwd == "" {
		return nil
	}
	probeMu.Lock()
	if e, ok := projCache[cwd]; ok && time.Since(e.t) < 3*time.Second {
		probeMu.Unlock()
		return e.v
	}
	probeMu.Unlock()
	var rs []rule
	L := ci.lang
	add := func(text string, w float64, why string) { rs = append(rs, rule{text, "rule", why, w}) }

	if exists(cwd, "package.json") {
		pm := "npm"
		switch {
		case exists(cwd, "pnpm-lock.yaml"):
			pm = "pnpm"
		case exists(cwd, "yarn.lock"):
			pm = "yarn"
		case exists(cwd, "bun.lockb") || exists(cwd, "bun.lock"):
			pm = "bun"
		}
		if !exists(cwd, "node_modules") {
			add(pm+" install", 4.5, T(L, "projectDeps", "Node"))
		}
		var pj struct {
			Scripts map[string]string `json:"scripts"`
		}
		if b, err := os.ReadFile(filepath.Join(cwd, "package.json")); err == nil && json.Unmarshal(b, &pj) == nil {
			pri := map[string]float64{"dev": 2.6, "start": 2.2, "build": 1.8, "test": 1.6, "lint": 1.0, "preview": 1.0}
			for name := range pj.Scripts {
				w, ok := pri[name]
				if !ok {
					w = 0.6
				}
				run := pm + " run " + name
				if pm == "npm" && (name == "start" || name == "test") {
					run = "npm " + name
				}
				add(run, w, T(L, "script", "package.json", name))
			}
		}
	}
	if exists(cwd, "Cargo.toml") {
		add("cargo run", 2.2, T(L, "project", "Rust"))
		add("cargo build", 2.0, T(L, "project", "Rust"))
		add("cargo test", 1.4, T(L, "project", "Rust"))
	}
	if exists(cwd, "go.mod") {
		add("go build ./...", 2.0, T(L, "project", "Go"))
		add("go test ./...", 1.8, T(L, "project", "Go"))
		add("go run .", 1.6, T(L, "project", "Go"))
	}
	if exists(cwd, "Makefile") || exists(cwd, "makefile") {
		add("make", 2.2, T(L, "project", "Make"))
	}
	if exists(cwd, "requirements.txt") {
		if !exists(cwd, ".venv") && !exists(cwd, "venv") {
			add("python3 -m venv .venv", 2.4, T(L, "projectDeps", "Python"))
		}
		add("pip install -r requirements.txt", 2.0, T(L, "project", "Python"))
	}
	if exists(cwd, "pyproject.toml") {
		switch {
		case exists(cwd, "uv.lock"):
			add("uv sync", 2.4, T(L, "project", "Python/uv"))
		case exists(cwd, "poetry.lock"):
			add("poetry install", 2.4, T(L, "project", "Poetry"))
		}
	}
	for _, f := range []string{"docker-compose.yml", "docker-compose.yaml", "compose.yml", "compose.yaml"} {
		if exists(cwd, f) {
			add("docker compose up -d", 2.2, T(L, "project", "Docker Compose"))
			break
		}
	}
	if exists(cwd, "Dockerfile") {
		add("docker build -t "+strings.ToLower(filepath.Base(cwd))+" .", 1.5, T(L, "project", "Docker"))
	}
	if exists(cwd, "CMakeLists.txt") {
		add("cmake -B build && cmake --build build", 1.8, T(L, "project", "CMake"))
	}
	probeMu.Lock()
	projCache[cwd] = cacheEntry[[]rule]{rs, time.Now()}
	probeMu.Unlock()
	return rs
}

var extractRe = regexp.MustCompile(`(?i)([^/\s]+?)\.(tar\.gz|tgz|tar\.xz|tar\.bz2|tar|zip)\b`)

// rules encode common-sense "what comes next" knowledge, blended with what you've learned.
func (m *Model) rules(ci ctxInfo) []rule {
	var rs []rule
	L := ci.lang
	add := func(text string, w float64, kind, why string) { rs = append(rs, rule{text, kind, why, w}) }
	last := ci.last
	toks, _, _ := Split(last)
	pi := ProgIndex(toks)
	prog := ""
	var args []string
	if pi >= 0 {
		prog = ProgName(toks[pi])
		for _, t := range toks[pi+1:] {
			if !strings.HasPrefix(t, "-") {
				args = append(args, t)
			}
		}
	}
	ok := ci.lastExit == 0
	posix := ci.shell != "pwsh" && runtime.GOOS != "windows"

	switch {
	case last == "":
	case ci.lastExit == 127:
		m.mu.RLock()
		fixed, _ := m.fixLast(last)
		m.mu.RUnlock()
		if fixed != "" {
			add(fixed, 9, "fix", T(L, "notFound"))
		}
	case !ok && ci.lastExit != 130 && posix && !strings.HasPrefix(last, "sudo ") && needsRoot(prog, args):
		add("sudo "+last, 7, "fix", T(L, "sudo"))
	}

	if ok && prog != "" {
		then := func(text string, w float64) { add(text, w, "rule", T(L, "thenRun", short(last))) }
		switch prog {
		case "git":
			if len(args) > 0 {
				switch args[0] {
				case "clone":
					if len(args) >= 2 {
						dir := strings.TrimSuffix(filepath.Base(strings.TrimRight(args[1], "/")), ".git")
						if len(args) >= 3 {
							dir = args[2]
						}
						if i := strings.LastIndex(dir, ":"); i >= 0 {
							dir = dir[i+1:]
						}
						add("cd "+Quote(dir, ci.shell), 8, "rule", T(L, "cloned"))
					}
				case "add":
					then(`git commit -m ""`, 3.5)
				case "commit":
					then("git push", 3.2)
				case "checkout", "switch":
					if hasFlag(toks, "-b", "-c") && len(args) >= 2 {
						add("git push -u origin "+args[len(args)-1], 3, "rule", T(L, "gitBranch"))
					}
				}
			}
		case "mkdir":
			if len(args) == 1 {
				add("cd "+Quote(args[0], ci.shell), 7, "rule", T(L, "mkdir"))
			}
		case "tar", "unzip":
			for _, a := range args {
				if mm := extractRe.FindStringSubmatch(filepath.Base(a)); mm != nil && isDir(filepath.Join(ci.cwd, mm[1])) {
					add("cd "+Quote(mm[1], ci.shell), 5, "rule", T(L, "extracted"))
				}
			}
		case "wget", "curl":
			for _, a := range args {
				if mm := extractRe.FindStringSubmatch(a); mm != nil {
					f := mm[1] + "." + mm[2]
					if exists(ci.cwd, f) {
						if strings.EqualFold(mm[2], "zip") {
							add("unzip "+Quote(f, ci.shell), 4.5, "rule", T(L, "extract"))
						} else {
							add("tar -xf "+Quote(f, ci.shell), 4.5, "rule", T(L, "extract"))
						}
					}
				}
			}
		case "npm", "pnpm", "yarn", "bun":
			if len(args) == 0 || args[0] == "install" || args[0] == "i" || args[0] == "ci" {
				then(prog+" run dev", 3.6)
			}
		case "cargo":
			if len(args) > 0 && args[0] == "build" {
				then("cargo run", 3.4)
			}
		case "docker":
			if len(args) >= 2 && args[0] == "compose" && args[1] == "up" {
				then("docker compose logs -f", 3)
			}
		case "python", "python3":
			if len(args) >= 2 && args[0] == "venv" {
				if posix {
					add("source "+args[1]+"/bin/activate", 6, "rule", T(L, "activate"))
				} else {
					add(args[1]+`\Scripts\Activate.ps1`, 6, "rule", T(L, "activate"))
				}
			}
		case "ssh-keygen":
			add("cat ~/.ssh/id_ed25519.pub", 4, "rule", T(L, "pubkey"))
		case "chmod":
			if len(args) >= 2 && strings.Contains(args[0], "x") {
				f := args[len(args)-1]
				if !strings.ContainsAny(f, `/\`) {
					f = "./" + f
				}
				add(f, 5, "rule", T(L, "runChmod"))
			}
		case "systemctl":
			if len(args) >= 2 && (args[0] == "start" || args[0] == "restart" || args[0] == "enable") {
				sudo := ""
				if strings.HasPrefix(last, "sudo ") {
					sudo = "sudo "
				}
				add(sudo+"systemctl status "+args[len(args)-1], 3, "rule", T(L, "status"))
			}
		case "vim", "nvim", "vi", "nano", "code", "micro", "hx", "emacs":
			if len(args) == 1 {
				f := args[0]
				run := ""
				switch strings.ToLower(filepath.Ext(f)) {
				case ".py":
					run = "python3 " + f
				case ".go":
					run = "go run " + f
				case ".sh":
					run = "bash " + f
				case ".js", ".mjs":
					run = "node " + f
				case ".ts":
					run = "npx tsx " + f
				case ".rb":
					run = "ruby " + f
				}
				if run != "" {
					add(run, 2.8, "rule", T(L, "runEdited"))
				}
			}
		}
		if cdLike[prog] {
			for _, r := range m.projectRules(ci) {
				r.w += 1.2 // just walked into a project: its commands are very likely
				rs = append(rs, r)
			}
		}
	}
	if !cdLike[prog] {
		rs = append(rs, m.projectRules(ci)...)
	}

	if gi := probeGit(ci.cwd); gi.repo {
		if n := gi.unstaged + gi.untracked; n > 0 {
			add("git add -A", 2.2, "rule", T(L, "gitDirty", n))
			add("git status", 1.6, "rule", T(L, "gitDirty", n))
			add("git diff", 1.0, "rule", T(L, "gitDirty", n))
		}
		if gi.staged > 0 {
			add(`git commit -m ""`, 3.4, "rule", T(L, "gitStaged", gi.staged))
		}
		if gi.ahead > 0 {
			add("git push", 3.4, "rule", T(L, "gitAhead", gi.ahead))
		}
		if gi.behind > 0 {
			add("git pull", 3.0, "rule", T(L, "gitBehind", gi.behind))
		}
		if gi.noUpstream && gi.branch != "" && gi.staged+gi.unstaged == 0 {
			add("git push -u origin "+gi.branch, 1.8, "rule", T(L, "gitBranch"))
		}
	}
	return rs
}

func hasFlag(toks []string, flags ...string) bool {
	for _, t := range toks {
		for _, f := range flags {
			if t == f {
				return true
			}
		}
	}
	return false
}

func needsRoot(prog string, args []string) bool {
	a0 := ""
	if len(args) > 0 {
		a0 = args[0]
	}
	switch prog {
	case "apt", "apt-get", "dnf", "yum", "zypper", "apk", "pacman", "emerge", "xbps-install", "snap", "port":
		return true
	case "systemctl":
		return a0 != "status" && a0 != "--user" && a0 != "list-units" && a0 != "is-active"
	case "mount", "umount", "modprobe", "ufw", "iptables", "useradd", "usermod", "chown", "reboot", "shutdown", "fdisk", "parted":
		return true
	case "npm", "pnpm", "yarn":
		return a0 == "install" || a0 == "i" || a0 == "add" // usually -g permission errors
	}
	return false
}
