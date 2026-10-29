// Package target keeps track of the things a request is about (a repository, a
// library, a URL, a file) and whether anything in the session actually looked
// at them.
//
// The environment probe answers "what is on this machine". This package answers
// the other half of "do not assume": an agent that writes about ffmpeg-skill
// should have read something about ffmpeg-skill first. It cannot tell whether
// the answer is correct. It can only tell whether a source was looked at.
package target

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

// Target is one thing named in a request.
type Target struct {
	Text string `json:"text"` // as written by the user, shortened
	Key  string `json:"key"`  // lowercase letters and digits only, used for matching
}

// MaxPerPrompt caps how many targets one request can add.
const MaxPerPrompt = 8

var (
	pasted    = regexp.MustCompile(`(?s)<pasted_content[^>]*>.*?</pasted_content[^>]*>`)
	fence     = regexp.MustCompile("(?s)```.*?```")
	urlRe     = regexp.MustCompile(`https?://[^\s<>"'` + "`" + `）」』】、。]+`)
	email     = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+`)
	backtick  = regexp.MustCompile("`([^`\\s]{2,80})`")
	quoted    = regexp.MustCompile(`[「『"“]([A-Za-z0-9][A-Za-z0-9 ._/+-]{1,60})[」』"”]`)
	ownerRepo = regexp.MustCompile(`(?:^|[^A-Za-z0-9._/-])([A-Za-z0-9][A-Za-z0-9_.-]*/[A-Za-z0-9][A-Za-z0-9_.-]*)`)
	compound  = regexp.MustCompile(`(?:^|[^A-Za-z0-9_./-])([A-Za-z][A-Za-z0-9]*(?:[-_.][A-Za-z0-9]+)+)`)
	// An ASCII name right before a Japanese "about / what is / look up" phrase:
	// "ffmpegについて調べて", "Tauriとは", "Astroって何".
	jaAbout = regexp.MustCompile(`([A-Za-z][A-Za-z0-9+#]{2,40})\s*(?:について|とは|って何|ってなに|を調べ|をしらべ|を比較|の(?:最新|仕様|料金|価格|バージョン|使い方|機能))`)
	// English: "research Tauri", "look up Astro", "what is Bun".
	enAbout = regexp.MustCompile(`(?i)\b(?:research|look\s+up|look\s+into|investigate|what\s+is|what's|tell\s+me\s+about)\s+([A-Za-z][A-Za-z0-9+#._-]{2,40})`)
	version = regexp.MustCompile(`^v?\d+(\.\d+)*$`)
)

// stop are words the patterns can catch that are not things to look up.
var stop = map[string]bool{
	"eg": true, "ie": true, "etc": true, "vs": true, "the": true, "this": true, "that": true,
	"it": true, "its": true, "a": true, "an": true, "andor": true, "inputoutput": true,
	"readwrite": true, "io": true, "yes": true, "no": true, "ok": true,
}

// common words that appear in pairs like "and/or" or "client/server";
// such a pair is not a repository name.
var common = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`and or not input output in out read write yes no on off true false
		he she him her his hers they them s he client server frontend backend front back end start stop
		open close get set put post up down left right before after old new this that all any some none
		day night am pm km h min sec mb gb kb tb unix linux mac windows win ios android web app apps`) {
		common[w] = true
	}
}

// Key reduces s to lowercase letters and digits, so "ffmpeg-skill",
// "FFmpeg Skill" and "ffmpeg_skill" all match each other.
func Key(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func urlKey(u string) string {
	u = strings.TrimRight(u, ".,;:!?)]}")
	u = strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://")
	u = strings.TrimPrefix(u, "www.")
	return Key(u)
}

// Extract finds the targets named in a request. It is deliberately
// conservative: plain words are taken only next to an "about / look up" phrase.
func Extract(prompt string) []Target {
	text := pasted.ReplaceAllString(prompt, " ")
	text = fence.ReplaceAllString(text, " ")
	var out []Target
	seen := map[string]bool{}
	add := func(raw, key string) {
		raw = strings.TrimSpace(raw)
		if len(out) >= MaxPerPrompt || len(key) < 2 || seen[key] || stop[key] {
			return
		}
		if version.MatchString(raw) || !hasLetter(raw) {
			return
		}
		for _, t := range out {
			if strings.Contains(t.Key, key) {
				return // a piece of a name already taken ("kit" of "lumen-kit")
			}
		}
		seen[key] = true
		if len(raw) > 80 {
			raw = raw[:80]
		}
		out = append(out, Target{Text: raw, Key: key})
	}
	for _, u := range urlRe.FindAllString(text, -1) {
		add(strings.TrimRight(u, ".,;:!?)]}"), urlKey(u))
	}
	text = urlRe.ReplaceAllString(text, " ")
	text = email.ReplaceAllString(text, " ")
	for _, m := range backtick.FindAllStringSubmatch(text, -1) {
		add(m[1], Key(m[1]))
	}
	for _, m := range quoted.FindAllStringSubmatch(text, -1) {
		add(m[1], Key(m[1]))
	}
	for _, m := range ownerRepo.FindAllStringSubmatch(text, -1) {
		s := m[1]
		a, b, _ := strings.Cut(strings.ToLower(s), "/")
		if !(common[a] && common[b]) {
			add(s, Key(s))
		}
	}
	for _, m := range compound.FindAllStringSubmatch(text, -1) {
		add(m[1], Key(m[1]))
	}
	for _, m := range jaAbout.FindAllStringSubmatch(text, -1) {
		add(m[1], Key(m[1]))
	}
	for _, m := range enAbout.FindAllStringSubmatch(text, -1) {
		add(strings.TrimRight(m[1], ".,;:!?"), Key(m[1]))
	}
	return out
}

func hasLetter(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

// words lowercases s and splits it into runs of letters and digits.
func words(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
	})
}

// Mentions reports whether text mentions the target as whole words, so "bun"
// is not found inside "bundle".
func Mentions(text string, t Target) bool {
	if t.Key == "" {
		return false
	}
	hay := " " + strings.Join(words(text), " ") + " "
	if tw := words(t.Text); len(tw) > 0 && strings.Contains(hay, " "+strings.Join(tw, " ")+" ") {
		return true
	}
	return strings.Contains(hay, " "+t.Key+" ")
}

// hedge matches an answer that already says it did not verify something.
var hedge = regexp.MustCompile(`(?i)(未確認|確認していません|確認できていません|確認できません|検証していません|分かりません|わかりません|知りません|情報がありません|情報を持っていません|not verified|unverified|haven't verified|have not verified|could(?:n't| not) (?:verify|confirm|find)|i don't know|i do not know|don't have (?:any |reliable )?information|no reliable information)`)

// Hedged reports whether text already admits that something is not verified,
// so sending it back would only repeat that.
func Hedged(text string) bool { return hedge.MatchString(text) }

// Log is the per-session record of targets. It is an append-only file of JSON
// lines, so tool calls that run at the same time cannot overwrite each other.
type Log struct{ Path string }

type entry struct {
	Kind string `json:"k"` // prompt | target | seen | warned
	Text string `json:"text,omitempty"`
	Key  string `json:"key"`
	How  string `json:"how,omitempty"`
}

// State is what the log says.
type State struct {
	Targets []Target
	Seen    map[string]string // key -> tool that looked at it
	Warned  map[string]bool
	Latest  []string // keys named in the most recent request
}

// Read loads the log. A missing file is an empty state.
func (l Log) Read() (*State, error) {
	st := &State{Seen: map[string]string{}, Warned: map[string]bool{}}
	f, err := os.Open(l.Path)
	if os.IsNotExist(err) {
		return st, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	have := map[string]bool{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		var e entry
		if json.Unmarshal(sc.Bytes(), &e) != nil || e.Key == "" {
			continue // a torn line; skip it
		}
		switch e.Kind {
		case "target":
			if !have[e.Key] {
				have[e.Key] = true
				st.Targets = append(st.Targets, Target{Text: e.Text, Key: e.Key})
			}
		case "seen":
			if _, ok := st.Seen[e.Key]; !ok {
				st.Seen[e.Key] = e.How
			}
		case "warned":
			st.Warned[e.Key] = true
		case "prompt":
			st.Latest = nil
			if e.Text != "" {
				st.Latest = strings.Split(e.Text, ",")
			}
		}
	}
	return st, sc.Err()
}

func (l Log) append(es []entry) error {
	if len(es) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(l.Path), 0o700); err != nil {
		return err
	}
	var b strings.Builder
	for _, e := range es {
		j, _ := json.Marshal(e)
		b.Write(j)
		b.WriteByte('\n')
	}
	f, err := os.OpenFile(l.Path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(b.String()); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// AddTargets records the targets named in a new request. Targets already
// known are listed again so that the log knows what the latest request named.
func (l Log) AddTargets(ts []Target) error {
	keys := make([]string, 0, len(ts))
	for _, t := range ts {
		keys = append(keys, t.Key)
	}
	es := []entry{{Kind: "prompt", Key: "p", Text: strings.Join(keys, ",")}}
	for _, t := range ts {
		es = append(es, entry{Kind: "target", Text: t.Text, Key: t.Key})
	}
	return l.append(es)
}

// MarkSeen records that a tool looked at these targets.
func (l Log) MarkSeen(keys []string, how string) error {
	var es []entry
	for _, k := range keys {
		es = append(es, entry{Kind: "seen", Key: k, How: how})
	}
	return l.append(es)
}

// MarkWarned records that the agent was already stopped once for these targets.
func (l Log) MarkWarned(keys []string) error {
	var es []entry
	for _, k := range keys {
		es = append(es, entry{Kind: "warned", Key: k})
	}
	return l.append(es)
}

// Unseen returns the targets no tool has looked at yet.
func (s *State) Unseen() []Target {
	var out []Target
	for _, t := range s.Targets {
		if _, ok := s.Seen[t.Key]; !ok {
			out = append(out, t)
		}
	}
	return out
}

// Touched returns the keys of unseen targets that a tool call's input or
// output mentions.
func (s *State) Touched(blobs ...[]byte) []string {
	unseen := s.Unseen()
	if len(unseen) == 0 {
		return nil
	}
	var hay strings.Builder
	for _, b := range blobs {
		if len(b) > 256*1024 {
			b = b[:256*1024]
		}
		hay.WriteString(Key(unescape(b)))
	}
	h := hay.String()
	var keys []string
	for _, t := range unseen {
		if strings.Contains(h, t.Key) {
			keys = append(keys, t.Key)
		}
	}
	return keys
}

// unescape turns a JSON value into plain text where it can, so "-" and
// friends do not hide a name.
func unescape(b []byte) string {
	var v any
	if json.Unmarshal(b, &v) != nil {
		return string(b)
	}
	var sb strings.Builder
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case string:
			sb.WriteString(x)
			sb.WriteByte(' ')
		case []any:
			for _, e := range x {
				walk(e)
			}
		case map[string]any:
			for k, e := range x {
				sb.WriteString(k)
				sb.WriteByte(' ')
				walk(e)
			}
		}
	}
	walk(v)
	return sb.String()
}
