package agent

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
)

var (
	thinkRe   = regexp.MustCompile(`(?is)<think(ing)?>.*?</think(ing)?>`)
	fenceRe   = regexp.MustCompile("(?s)```([a-zA-Z0-9_-]*)\\s*\\n(.*?)```")
	dollarRe  = regexp.MustCompile(`(?m)^\s*(?:\$|❯|>|PS>)\s+(\S.*)$`)
	errNoPlan = errors.New("model did not return a plan")
	placeRe   = regexp.MustCompile(`^\s*(\.{2,}|…|<[^>]*>|\[[^\]]*\]|string|command|shell command|todo|tbd)?\s*$`)
)

var rootCmdRe = regexp.MustCompile(`^(pacman\s+-[A-Za-z]*[SRU]|apt(-get)?\s+(install|remove|purge|upgrade|full-upgrade|dist-upgrade|update|autoremove)\b|dnf\s+(install|remove|upgrade|update|distro-sync)\b|yum\s+(install|remove|update|upgrade)\b|zypper\s|apk\s+(add|del|upgrade|update)\b|xbps-install|xbps-remove|emerge\s|snap\s+(install|remove|refresh)\b|systemctl\s+(start|stop|restart|enable|disable|daemon-reload)\s|mkinitcpio\s|grub-mkconfig\s)`)

// addSudo prefixes sudo where the OS requires root and the model forgot it.
// AUR helpers and Homebrew must never run as root, so they are left alone.
func addSudo(cmd string, flagged bool) string {
	c := strings.TrimSpace(cmd)
	if strings.HasPrefix(c, "sudo ") || strings.HasPrefix(c, "doas ") {
		return cmd
	}
	for _, never := range []string{"paru ", "yay ", "brew ", "flatpak --user", "npm ", "pip ", "cargo ", "go "} {
		if strings.HasPrefix(c, never) {
			return cmd
		}
	}
	if rootCmdRe.MatchString(c) || (flagged && !strings.ContainsAny(c, "|;&")) {
		return "sudo " + c
	}
	return cmd
}

// placeholder reports text a model copied from the format example instead of writing.
func placeholder(t string) bool { return placeRe.MatchString(strings.ToLower(t)) }

// usable rejects plans that are empty shells or echo the schema back.
func usable(p Plan) bool {
	for _, st := range p.Steps {
		if placeholder(st.Command) || placeholder(st.Title) {
			return false
		}
	}
	if len(p.Steps) == 0 && len(p.Lookup) == 0 {
		return !placeholder(p.Notes) || !placeholder(p.Summary)
	}
	return true
}

// jsonObjects returns every balanced top-level {...} in s, honouring strings and escapes.
func jsonObjects(s string) []string {
	var out []string
	depth, start := 0, -1
	inStr, esc := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			if depth > 0 {
				inStr = true
			}
		case '{':
			if depth == 0 {
				start = i
			}
			depth++
		case '}':
			if depth > 0 {
				depth--
				if depth == 0 && start >= 0 {
					out = append(out, s[start:i+1])
					start = -1
				}
			}
		}
	}
	return out
}

func str(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			switch x := v.(type) {
			case string:
				if strings.TrimSpace(x) != "" {
					return strings.TrimSpace(x)
				}
			case []any: // some models return a command as a list of lines
				var parts []string
				for _, p := range x {
					if s, ok := p.(string); ok {
						parts = append(parts, s)
					}
				}
				if len(parts) > 0 {
					return strings.Join(parts, " && ")
				}
			}
		}
	}
	return ""
}

func boolean(m map[string]any, k string) bool {
	switch x := m[k].(type) {
	case bool:
		return x
	case string:
		return x == "true" || x == "yes"
	}
	return false
}

// planFromMap accepts the schema we ask for and the common variations small models produce.
func planFromMap(m map[string]any) (Plan, bool) {
	p := Plan{Summary: str(m, "summary", "plan", "description", "title"), Notes: str(m, "notes", "note", "message", "explanation")}
	if lk, ok := m["lookup"].([]any); ok {
		for _, v := range lk {
			if s, ok := v.(string); ok && s != "" {
				p.Lookup = append(p.Lookup, s)
			}
		}
	}
	var raw []any
	for _, k := range []string{"steps", "commands", "plan_steps", "actions", "tasks"} {
		if v, ok := m[k].([]any); ok {
			raw = v
			break
		}
	}
	for _, it := range raw {
		switch x := it.(type) {
		case string:
			if strings.TrimSpace(x) != "" {
				p.Steps = append(p.Steps, Step{Title: x, Command: strings.TrimSpace(x), Risk: "medium"})
			}
		case map[string]any:
			cmd := str(x, "command", "cmd", "shell", "run", "script", "code")
			if cmd == "" {
				continue
			}
			title := str(x, "title", "name", "step", "description", "summary")
			if title == "" {
				title = cmd
			}
			p.Steps = append(p.Steps, Step{
				Title:   title,
				Command: cmd,
				Explain: str(x, "explain", "explanation", "why", "details", "note"),
				Risk:    strings.ToLower(str(x, "risk", "danger", "level")),
				Sudo:    boolean(x, "sudo") || strings.HasPrefix(cmd, "sudo "),
			})
		}
	}
	ok := len(p.Steps) > 0 || len(p.Lookup) > 0 || p.Notes != "" || p.Summary != ""
	return p, ok
}

// commandsFromText is the last resort: shell code blocks or "$ cmd" lines become steps.
func commandsFromText(s string) []Step {
	var steps []Step
	add := func(cmd string) {
		cmd = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(cmd), "$ "))
		if cmd == "" || strings.HasPrefix(cmd, "#") {
			return
		}
		steps = append(steps, Step{Title: cmd, Command: cmd, Risk: "medium", Sudo: strings.HasPrefix(cmd, "sudo ")})
	}
	for _, m := range fenceRe.FindAllStringSubmatch(s, -1) {
		lang := strings.ToLower(m[1])
		if lang != "" && !strings.Contains("bash sh shell zsh fish console powershell pwsh ps1 cmd terminal", lang) {
			continue
		}
		for _, ln := range strings.Split(m[2], "\n") {
			ln = strings.TrimSpace(ln)
			if strings.HasSuffix(ln, `\`) { // keep continuation lines together
				ln = strings.TrimSuffix(ln, `\`)
			}
			add(ln)
		}
	}
	if len(steps) == 0 {
		for _, m := range dollarRe.FindAllStringSubmatch(s, -1) {
			add(m[1])
		}
	}
	return steps
}

// parsePlan turns whatever the model said into a Plan.
func parsePlan(s string) (Plan, error) {
	s = strings.TrimSpace(thinkRe.ReplaceAllString(s, ""))
	if s == "" {
		return Plan{}, errNoPlan
	}
	// 1. JSON objects, largest plan wins (models sometimes echo a schema first)
	best, found := Plan{}, false
	for _, obj := range jsonObjects(s) {
		var m map[string]any
		if json.Unmarshal([]byte(obj), &m) != nil {
			continue
		}
		if p, ok := planFromMap(m); ok && usable(p) && (!found || len(p.Steps) > len(best.Steps)) {
			best, found = p, true
		}
	}
	if found && len(best.Steps) > 0 {
		return best, nil
	}
	// 2. a bare JSON array of steps
	if i, j := strings.Index(s, "["), strings.LastIndex(s, "]"); i >= 0 && j > i {
		var arr []any
		if json.Unmarshal([]byte(s[i:j+1]), &arr) == nil {
			if p, ok := planFromMap(map[string]any{"steps": arr}); ok && len(p.Steps) > 0 && usable(p) {
				return p, nil
			}
		}
	}
	// 3. prose with shell code
	if steps := commandsFromText(s); len(steps) > 0 {
		return Plan{Steps: steps}, nil
	}
	// 4. a real answer with no steps (a question back, or a refusal with a reason)
	if found {
		return best, nil
	}
	return Plan{}, errNoPlan
}

var requestWords = map[string]bool{"the": true, "a": true, "an": true, "my": true, "me": true, "to": true, "for": true, "and": true, "with": true, "please": true, "on": true, "of": true, "in": true, "from": true, "all": true, "some": true, "this": true, "that": true, "it": true, "latest": true, "new": true, "how": true, "what": true, "i": true, "want": true, "need": true, "can": true, "you": true}

// LooksLikeRequest decides whether an unknown "command" is really a sentence for smart mode:
// any Arabic, or 3+ plain English words that include ordinary request words.
func LooksLikeRequest(words []string) bool {
	line := strings.Join(words, " ")
	for _, r := range line {
		if r >= 0x0600 && r <= 0x06FF {
			return true
		}
	}
	if len(words) < 3 {
		return false
	}
	hits := 0
	for _, w := range words {
		if strings.HasPrefix(w, "-") || strings.ContainsAny(w, "/\\=|<>$`;&") {
			return false
		}
		if requestWords[strings.ToLower(strings.Trim(w, ",.?!"))] {
			hits++
		}
	}
	return hits >= 1
}
