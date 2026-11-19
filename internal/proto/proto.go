// Package proto holds the JSON-lines wire types shared by the daemon and its clients.
package proto

type Request struct {
	Op     string `json:"op"`
	Token  string `json:"token,omitempty"`
	Buffer string `json:"buffer,omitempty"`
	Cwd    string `json:"cwd,omitempty"`
	Sess   string `json:"sess,omitempty"`
	Shell  string `json:"shell,omitempty"`
	Cmd    string `json:"cmd,omitempty"`
	Exit   int    `json:"exit,omitempty"`
	Limit  int    `json:"limit,omitempty"`
	Query  string `json:"query,omitempty"`
	Dirs   bool   `json:"dirs,omitempty"`
	Time   int64  `json:"time,omitempty"`
	Lang   string `json:"lang,omitempty"`
}

type Suggestion struct {
	Text   string  `json:"text"`
	Score  float64 `json:"score"`
	Kind   string  `json:"kind"`   // history | next | path | rule | fix
	Reason string  `json:"reason"` // human readable "why"
}

type Match struct {
	Path  string  `json:"path"`
	Dir   bool    `json:"dir"`
	Size  int64   `json:"size"`
	Mod   int64   `json:"mod"`
	Score float64 `json:"score"`
}

type Response struct {
	OK          bool           `json:"ok"`
	Error       string         `json:"error,omitempty"`
	Suggestions []Suggestion   `json:"suggestions,omitempty"`
	Matches     []Match        `json:"matches,omitempty"`
	Stats       map[string]any `json:"stats,omitempty"`
}
