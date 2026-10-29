// Package store keeps one probe record per agent session on disk.
package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Status values for a fact. Only these three exist.
const (
	OK      = "ok"
	Missing = "missing"
	Unknown = "unknown"
)

// Fact is one thing the probe checked.
type Fact struct {
	Key    string `json:"key"`
	Status string `json:"status"`
	Value  string `json:"value,omitempty"`
	How    string `json:"how,omitempty"`
	Why    string `json:"why,omitempty"`
}

// State is everything remembered for one session.
type State struct {
	Session       string            `json:"session"`
	TakenAt       time.Time         `json:"taken_at"`
	Cwd           string            `json:"cwd"`
	Facts         []Fact            `json:"facts"`
	Commands      map[string]string `json:"commands,omitempty"` // command name -> ok|missing|allowed
	Denied        map[string]int    `json:"denied,omitempty"`   // command name -> times denied
	ProbeFailures int               `json:"probe_failures,omitempty"`
}

// Fact returns the fact with the given key, if any.
func (s *State) Fact(key string) (Fact, bool) {
	for _, f := range s.Facts {
		if f.Key == key {
			return f, true
		}
	}
	return Fact{}, false
}

// SetFacts replaces facts that share a key and appends new ones, keeping order stable.
func (s *State) SetFacts(fs []Fact) {
	idx := map[string]int{}
	for i, f := range s.Facts {
		idx[f.Key] = i
	}
	for _, f := range fs {
		if i, ok := idx[f.Key]; ok {
			s.Facts[i] = f
			continue
		}
		idx[f.Key] = len(s.Facts)
		s.Facts = append(s.Facts, f)
	}
}

// DropPrefix removes every fact whose key starts with prefix.
func (s *State) DropPrefix(prefix string) {
	kept := s.Facts[:0]
	for _, f := range s.Facts {
		if !strings.HasPrefix(f.Key, prefix) {
			kept = append(kept, f)
		}
	}
	s.Facts = kept
}

// Dir returns the directory where session records live.
func Dir() (string, error) {
	if d := os.Getenv("ASSUME_NOTHING_DATA"); d != "" {
		return filepath.Join(d, "sessions"), nil
	}
	if d := os.Getenv("CLAUDE_PLUGIN_DATA"); d != "" {
		return filepath.Join(d, "sessions"), nil
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "assume-nothing", "sessions"), nil
}

// safeName turns a session id into a file name that cannot escape the directory.
func safeName(session string) string {
	var b strings.Builder
	for _, r := range session {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
		if b.Len() >= 128 {
			break
		}
	}
	if b.Len() == 0 {
		return "default"
	}
	return b.String()
}

func path(session string) (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, safeName(session)+".json"), nil
}

// TargetLogPath is where the session's list of looked-up targets is kept.
func TargetLogPath(session string) (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, safeName(session)+".targets.log"), nil
}

// Lock serializes changes to one session's record across hook processes that
// run at the same time (parallel tool calls). It waits up to two seconds and
// then goes ahead anyway: a lost update is better than a stuck agent.
func Lock(session string) (unlock func()) {
	p, err := path(session)
	if err != nil {
		return func() {}
	}
	lp := p + ".lock"
	_ = os.MkdirAll(filepath.Dir(lp), 0o700)
	deadline := time.Now().Add(2 * time.Second)
	for {
		f, err := os.OpenFile(lp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			f.Close()
			return func() { os.Remove(lp) }
		}
		if info, serr := os.Stat(lp); serr == nil && time.Since(info.ModTime()) > 10*time.Second {
			os.Remove(lp) // left behind by a process that died
			continue
		}
		if time.Now().After(deadline) {
			return func() {}
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Latest returns the id of the most recently updated session record.
func Latest() (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	entries, err := os.ReadDir(d)
	if err != nil {
		return "", err
	}
	var best string
	var bestT time.Time
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		if info, err := e.Info(); err == nil && info.ModTime().After(bestT) {
			best, bestT = strings.TrimSuffix(e.Name(), ".json"), info.ModTime()
		}
	}
	if best == "" {
		return "", ErrNotFound
	}
	return best, nil
}

// LastPath is where the agent's latest answer is kept for agents whose stop
// hook does not carry it (Cursor sends it in afterAgentResponse instead).
func LastPath(session string) (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, safeName(session)+".last.log"), nil
}

// ErrNotFound means no record exists yet for the session.
var ErrNotFound = errors.New("no probe record for this session")

// Load reads the record for a session.
func Load(session string) (*State, error) {
	p, err := path(session)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var s State
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	if s.Commands == nil {
		s.Commands = map[string]string{}
	}
	if s.Denied == nil {
		s.Denied = map[string]int{}
	}
	return &s, nil
}

// Save writes the record atomically.
func Save(s *State) error {
	p, err := path(s.Session)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".tmp-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), p)
}

// Prune deletes session records older than maxAge, so the folder does not grow forever.
func Prune(maxAge time.Duration) {
	d, err := Dir()
	if err != nil {
		return
	}
	entries, err := os.ReadDir(d)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-maxAge)
	for _, e := range entries {
		if e.IsDir() || !(strings.HasSuffix(e.Name(), ".json") || strings.HasSuffix(e.Name(), ".log") || strings.HasPrefix(e.Name(), ".tmp-")) {
			continue
		}
		if info, err := e.Info(); err == nil && info.ModTime().Before(cutoff) {
			os.Remove(filepath.Join(d, e.Name()))
		}
	}
}

// New returns an empty record for a session.
func New(session, cwd string) *State {
	return &State{
		Session:  session,
		Cwd:      cwd,
		Commands: map[string]string{},
		Denied:   map[string]int{},
	}
}
