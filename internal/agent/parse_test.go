package agent

import "testing"

func TestParsePlanVariants(t *testing.T) {
	cases := map[string]struct {
		in    string
		steps int
		first string
	}{
		"strict":     {`{"summary":"s","steps":[{"title":"t","command":"echo 1","risk":"low"}]}`, 1, "echo 1"},
		"think+text": {"<think>hmm {not json}</think>Sure! {\"steps\":[{\"name\":\"x\",\"cmd\":\"ls\"}]} hope it helps", 1, "ls"},
		"commands":   {`{"commands":["sudo pacman -Syu --noconfirm","claude --version"]}`, 2, "sudo pacman -Syu --noconfirm"},
		"array":      {`[{"title":"a","command":"uname -a"}]`, 1, "uname -a"},
		"codeblock":  {"Run these:\n```bash\nsudo apt update\nsudo apt install -y htop\n```", 2, "sudo apt update"},
		"dollar":     {"First:\n$ brew update\nthen\n$ brew install wget", 2, "brew update"},
		"zero steps": {`{"summary":"unclear","steps":[],"notes":"which game?"}`, 0, ""},
	}
	for name, c := range cases {
		p, err := parsePlan(c.in)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if len(p.Steps) != c.steps || (c.steps > 0 && p.Steps[0].Command != c.first) {
			t.Errorf("%s: got %+v", name, p.Steps)
		}
	}
}

func TestParsePlanRejectsJunk(t *testing.T) {
	for name, in := range map[string]string{
		"empty":       "",
		"prose":       "I cannot help with that right now.",
		"template":    `{"summary":"...","steps":[{"title":"...","command":"...","explain":"..."}],"notes":"..."}`,
		"angle hints": `{"summary":"<one sentence>","steps":[{"title":"<short title>","command":"<the real shell command>"}]}`,
		"truncated":   `{"summary": "محددة محد`,
	} {
		if p, err := parsePlan(in); err == nil {
			t.Errorf("%s: accepted %+v", name, p)
		}
	}
}

func TestAddSudo(t *testing.T) {
	cases := map[string]string{
		"pacman -Syu --noconfirm":           "sudo pacman -Syu --noconfirm",
		"pacman -Qi supertux":               "pacman -Qi supertux",
		"sudo apt install -y htop":          "sudo apt install -y htop",
		"apt install -y htop":               "sudo apt install -y htop",
		"paru -S --noconfirm visual-studio": "paru -S --noconfirm visual-studio",
		"brew install wget":                 "brew install wget",
		"systemctl restart sshd":            "sudo systemctl restart sshd",
		"which supertux":                    "which supertux",
	}
	for in, want := range cases {
		if got := addSudo(in, false); got != want {
			t.Errorf("addSudo(%q) = %q, want %q", in, got, want)
		}
	}
	if got := addSudo("make install", true); got != "sudo make install" {
		t.Errorf("flagged step: %q", got)
	}
}

func TestLooksLikeRequest(t *testing.T) {
	yes := [][]string{{"نزلي", "لعبة"}, {"update", "the", "system", "and", "install", "claude", "code"}, {"install", "docker", "for", "me"}}
	no := [][]string{{"gti", "push"}, {"gti", "push", "origin", "main"}, {"kubeclt", "get", "pods", "-n", "prod"}, {"foo"}, {"cp", "a/b", "to", "c"}}
	for _, w := range yes {
		if !LooksLikeRequest(w) {
			t.Errorf("want request: %v", w)
		}
	}
	for _, w := range no {
		if LooksLikeRequest(w) {
			t.Errorf("not a request: %v", w)
		}
	}
}
