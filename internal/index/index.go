// Package index keeps a compact, searchable map of the user's folders and files.
package index

import (
	"encoding/gob"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"hads/internal/config"
	"hads/internal/paths"
	"hads/internal/proto"
)

type Entry struct {
	Path  string
	LName string // lower-cased base name
	Dir   bool
	Size  int64
	Mod   int64
	Depth uint8
}

type Index struct {
	mu       sync.RWMutex
	entries  []Entry
	built    time.Time
	scanning atomic.Bool
	lastDur  time.Duration
}

func New() *Index { return &Index{} }

func (ix *Index) Len() int {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return len(ix.entries)
}

func (ix *Index) Built() time.Time {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return ix.built
}

func (ix *Index) Scanning() bool              { return ix.scanning.Load() }
func (ix *Index) LastDuration() time.Duration { return ix.lastDur }

// Scan walks the configured roots. It is throttled so it never hogs disk or CPU.
func (ix *Index) Scan(cfg *config.Config) error {
	if !ix.scanning.CompareAndSwap(false, true) {
		return nil
	}
	defer ix.scanning.Store(false)
	start := time.Now()
	names, abs := cfg.ExcludeSets()
	out := make([]Entry, 0, 1<<14)
	seen := 0
	for _, root := range cfg.Roots {
		root = filepath.Clean(paths.Expand(root))
		base := strings.Count(root, string(filepath.Separator))
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				if d != nil && d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if len(out) >= cfg.MaxFiles {
				return fs.SkipAll
			}
			name := d.Name()
			depth := strings.Count(p, string(filepath.Separator)) - base
			if d.IsDir() && p != root {
				if names[name] || abs[p] || depth > cfg.MaxDepth {
					return fs.SkipDir
				}
				// hidden dirs: index the name, but only descend into a few useful ones
				if strings.HasPrefix(name, ".") && name != ".config" && name != ".local" && name != ".ssh" {
					out = append(out, Entry{Path: p, LName: strings.ToLower(name), Dir: true, Depth: uint8(depth)})
					return fs.SkipDir
				}
			}
			if d.Type()&fs.ModeSymlink != 0 {
				return nil
			}
			if p == root {
				return nil
			}
			e := Entry{Path: p, LName: strings.ToLower(name), Dir: d.IsDir(), Depth: uint8(min(depth, 255))}
			if info, err := d.Info(); err == nil {
				e.Mod = info.ModTime().Unix()
				if !e.Dir {
					e.Size = info.Size()
				}
			}
			out = append(out, e)
			seen++
			if seen%4000 == 0 {
				time.Sleep(15 * time.Millisecond) // be gentle
			}
			return nil
		})
	}
	ix.mu.Lock()
	ix.entries = out
	ix.built = time.Now()
	ix.lastDur = time.Since(start)
	ix.mu.Unlock()
	return nil
}

type snapshot struct {
	Built   time.Time
	Entries []Entry
}

func (ix *Index) Save(path string) error {
	ix.mu.RLock()
	snap := snapshot{Built: ix.built, Entries: ix.entries}
	ix.mu.RUnlock()
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if err := gob.NewEncoder(f).Encode(snap); err != nil {
		f.Close()
		return err
	}
	f.Close()
	return os.Rename(tmp, path)
}

func (ix *Index) Load(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	var snap snapshot
	if err := gob.NewDecoder(f).Decode(&snap); err != nil {
		return err
	}
	ix.mu.Lock()
	ix.entries, ix.built = snap.Entries, snap.Built
	ix.mu.Unlock()
	return nil
}

// Add registers a path the daemon learned about after the last scan (e.g. a new mkdir).
func (ix *Index) Add(p string) {
	info, err := os.Stat(p)
	if err != nil {
		return
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	for i := len(ix.entries) - 1; i >= 0 && i >= len(ix.entries)-64; i-- {
		if ix.entries[i].Path == p {
			return
		}
	}
	e := Entry{Path: p, LName: strings.ToLower(filepath.Base(p)), Dir: info.IsDir(), Mod: info.ModTime().Unix()}
	if !e.Dir {
		e.Size = info.Size()
	}
	ix.entries = append(ix.entries, e)
}

type SearchOpts struct {
	DirsOnly  bool
	FilesOnly bool
	Cwd       string
	Limit     int
	Boost     func(path string) float64 // frecency from the predictor
}

// Score how well name matches q (both lower-case). 0 = no match.
func matchScore(name, q string) float64 {
	switch {
	case q == "":
		return 0.5
	case name == q:
		return 10
	case strings.HasPrefix(name, q):
		return 7 + float64(len(q))/float64(len(name)+1)
	}
	if i := strings.Index(name, q); i >= 0 {
		if i > 0 && strings.ContainsRune(" -_.()[]", rune(name[i-1])) {
			return 5
		}
		return 3.5
	}
	if len(q) >= 3 && subsequence(name, q) {
		return 1.2
	}
	return 0
}

func subsequence(s, q string) bool {
	j := 0
	for i := 0; i < len(s) && j < len(q); i++ {
		if s[i] == q[j] {
			j++
		}
	}
	return j == len(q)
}

// Search ranks entries by name match, recency, closeness to cwd and how often you use them.
func (ix *Index) Search(query string, o SearchOpts) []proto.Match {
	q := strings.ToLower(strings.TrimSpace(query))
	q = strings.TrimPrefix(q, "./")
	var dirHint []string
	if strings.ContainsAny(q, `/\`) {
		q = strings.ReplaceAll(q, `\`, "/")
		parts := strings.Split(strings.Trim(q, "/"), "/")
		q = parts[len(parts)-1]
		for _, p := range parts[:len(parts)-1] {
			if p != "" && p != "~" && p != "." && p != ".." {
				dirHint = append(dirHint, p)
			}
		}
	}
	if o.Limit <= 0 {
		o.Limit = 10
	}
	now := time.Now().Unix()
	cwd := filepath.Clean(o.Cwd)
	sep := string(filepath.Separator)

	type hit struct {
		e *Entry
		s float64
	}
	var hits []hit
	ix.mu.RLock()
	for i := range ix.entries {
		e := &ix.entries[i]
		if (o.DirsOnly && !e.Dir) || (o.FilesOnly && e.Dir) {
			continue
		}
		m := matchScore(e.LName, q)
		if m == 0 {
			continue
		}
		if len(dirHint) > 0 {
			lp := strings.ToLower(filepath.Dir(e.Path))
			ok := true
			for _, h := range dirHint {
				if !strings.Contains(lp, h) {
					ok = false
					break
				}
			}
			if !ok {
				continue
			}
			m += 1
		}
		s := m
		age := now - e.Mod
		switch {
		case age < 86400:
			s += 2
		case age < 7*86400:
			s += 1.2
		case age < 30*86400:
			s += 0.6
		}
		if o.Cwd != "" {
			parent := filepath.Dir(e.Path)
			if parent == cwd {
				s += 3.5
			} else if strings.HasPrefix(e.Path, cwd+sep) {
				s += 2 - 0.15*float64(strings.Count(e.Path[len(cwd):], sep))
			}
		}
		s -= 0.08 * float64(e.Depth)
		if strings.HasPrefix(e.LName, ".") && !strings.HasPrefix(q, ".") {
			s -= 1.5
		}
		if o.Boost != nil {
			s += o.Boost(e.Path)
		}
		hits = append(hits, hit{e, s})
	}
	ix.mu.RUnlock()

	sort.Slice(hits, func(i, j int) bool { return hits[i].s > hits[j].s })
	if len(hits) > o.Limit {
		hits = hits[:o.Limit]
	}
	out := make([]proto.Match, len(hits))
	for i, h := range hits {
		out[i] = proto.Match{Path: h.e.Path, Dir: h.e.Dir, Size: h.e.Size, Mod: h.e.Mod, Score: h.s}
	}
	return out
}

// TopDirs lists the first-level folders under home, for the agent's context.
func (ix *Index) TopDirs(limit int) []string {
	home := paths.Home()
	var out []string
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	for _, e := range ix.entries {
		if e.Dir && e.Depth == 1 && filepath.Dir(e.Path) == home && !strings.HasPrefix(e.LName, ".") {
			out = append(out, filepath.Base(e.Path))
			if len(out) >= limit {
				break
			}
		}
	}
	return out
}
