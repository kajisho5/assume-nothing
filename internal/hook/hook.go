// Package hook answers the lifecycle hooks of coding agents.
//
// The input and output shapes follow Claude Code's hooks (SessionStart,
// PreToolUse, PostToolUse). Codex CLI uses the same field names for these events.
package hook

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/kajisho5/assume-nothing/internal/gate"
	"github.com/kajisho5/assume-nothing/internal/probe"
	"github.com/kajisho5/assume-nothing/internal/render"
	"github.com/kajisho5/assume-nothing/internal/store"
)

// Input is what the agent sends on stdin.
type Input struct {
	SessionID     string          `json:"session_id"`
	Cwd           string          `json:"cwd"`
	HookEventName string          `json:"hook_event_name"`
	Source        string          `json:"source"`
	ToolName      string          `json:"tool_name"`
	ToolInput     json.RawMessage `json:"tool_input"`
}

// Output is what the hook prints on stdout. Nil means "say nothing".
type Output struct {
	HookSpecificOutput *Specific `json:"hookSpecificOutput,omitempty"`
}

// Specific carries the event-specific answer.
//
// PermissionDecision is only ever "deny". The hook never answers "allow",
// because that would skip the user's own permission prompts.
type Specific struct {
	HookEventName            string `json:"hookEventName"`
	AdditionalContext        string `json:"additionalContext,omitempty"`
	PermissionDecision       string `json:"permissionDecision,omitempty"`
	PermissionDecisionReason string `json:"permissionDecisionReason,omitempty"`
}

// Now is the clock; tests replace it.
var Now = time.Now

// Strict reports whether a broken probe should keep blocking tools.
func Strict() bool {
	for _, k := range []string{"ASSUME_NOTHING_STRICT", "CLAUDE_PLUGIN_OPTION_STRICT"} {
		switch strings.ToLower(os.Getenv(k)) {
		case "1", "true", "yes", "on":
			return true
		}
	}
	return false
}

// DeepCommand is how the agent can run deeper checks; set by main.
var DeepCommand = "assume-nothing deep"

func cwdOf(in Input) string {
	if in.Cwd != "" {
		return in.Cwd
	}
	if d := os.Getenv("CLAUDE_PROJECT_DIR"); d != "" {
		return d
	}
	d, _ := os.Getwd()
	return d
}

func fresh(in Input) *store.State {
	st := store.New(in.SessionID, cwdOf(in))
	st.TakenAt = Now()
	st.Facts = probe.Environment(st.Cwd)
	return st
}

// SessionStart probes the environment and hands the result to the agent.
func SessionStart(in Input) *Output {
	st := fresh(in)
	if old, err := store.Load(in.SessionID); err == nil {
		for k, v := range old.Commands {
			if v == store.OK {
				st.Commands[k] = v
			}
		}
	}
	if err := store.Save(st); err != nil {
		fmt.Fprintln(os.Stderr, "assume-nothing: could not save the probe record:", err)
	}
	store.Prune(30 * 24 * time.Hour)
	return &Output{&Specific{HookEventName: "SessionStart", AdditionalContext: render.Snapshot(st, DeepCommand)}}
}

func bashCommand(in Input) string {
	if in.ToolName != "Bash" {
		return ""
	}
	var ti struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal(in.ToolInput, &ti)
	return ti.Command
}

func callsSelf(cmd string) bool {
	return strings.Contains(cmd, gate.Self)
}

// PreToolUse blocks tool calls until a probe exists, and blocks commands that are not installed.
func PreToolUse(in Input) *Output {
	cmd := bashCommand(in)
	if callsSelf(cmd) {
		return nil
	}
	st, err := store.Load(in.SessionID)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			fmt.Fprintln(os.Stderr, "assume-nothing: could not read the probe record:", err)
		}
		st = fresh(in)
		snap := render.Snapshot(st, DeepCommand)
		if serr := store.Save(st); serr != nil {
			fmt.Fprintln(os.Stderr, "assume-nothing: could not save the probe record:", serr)
			if Strict() {
				return deny("assume-nothing: the probe record cannot be saved, and strict mode is on, so tools stay blocked. Fix the plugin data folder or turn strict mode off.\n\n" + snap)
			}
			// Without a record we cannot remember that we already blocked once,
			// so hand over the facts and let the call through.
			return withContext("PreToolUse", snap)
		}
		return deny("assume-nothing: this session had no environment probe yet, so one was just run. Read it, then make the same call again.\n\n" + snap)
	}

	var notes []string
	cwd := cwdOf(in)
	if cwd != "" && cwd != st.Cwd {
		ctx, cancel := context.WithTimeout(context.Background(), probe.Budget)
		tf := probe.TargetFacts(ctx, cwd)
		cancel()
		st.SetFacts(tf)
		st.Cwd = cwd
		notes = append(notes, "assume-nothing: the working folder changed. New folder facts:\n"+render.Facts(tf))
	}

	var out *Output
	if cmd != "" {
		if missing := gate.MissingCommands(cmd, st); len(missing) > 0 {
			out = deny(missingReason(missing, st))
		}
	}
	if err := store.Save(st); err != nil {
		fmt.Fprintln(os.Stderr, "assume-nothing: could not save the probe record:", err)
	}
	if out != nil {
		if len(notes) > 0 {
			out.HookSpecificOutput.PermissionDecisionReason = strings.Join(notes, "\n") + "\n" + out.HookSpecificOutput.PermissionDecisionReason
		}
		return out
	}
	if len(notes) > 0 {
		return withContext("PreToolUse", strings.Join(notes, "\n"))
	}
	return nil
}

func missingReason(missing []string, st *store.State) string {
	var b strings.Builder
	fmt.Fprintf(&b, "assume-nothing: not installed (PATH lookup): %s.\n", strings.Join(missing, ", "))
	b.WriteString("Do not assume it exists. Use an installed alternative, install it first, or check it with: ")
	fmt.Fprintf(&b, "%s tool %s\n", DeepCommand, missing[0])
	b.WriteString("If you are sure it exists (an alias, a shell function, or a PATH this check cannot see), run the same command again and it will go through.\n")
	var have []string
	for _, f := range st.Facts {
		if f.Status == store.OK && (strings.HasPrefix(f.Key, "lang.") || strings.HasPrefix(f.Key, "tool.") || strings.HasPrefix(f.Key, "pm.")) {
			have = append(have, f.Key[strings.IndexByte(f.Key, '.')+1:])
		}
	}
	if len(have) > 0 {
		fmt.Fprintf(&b, "Installed (from the session probe): %s\n", strings.Join(have, ", "))
	}
	return render.Cap(b.String(), render.Limit)
}

// PostToolUse re-checks tools after something was installed and reports what changed.
func PostToolUse(in Input) *Output {
	cmd := bashCommand(in)
	if cmd == "" || !gate.InstallsSomething(cmd) {
		return nil
	}
	st, err := store.Load(in.SessionID)
	if err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), probe.Budget)
	after := probe.ToolFacts(ctx, probe.LoadConfig(st.Cwd).Tools)
	cancel()
	var changed []store.Fact
	for _, f := range after {
		old, ok := st.Fact(f.Key)
		if !ok || old.Status != f.Status || old.Value != f.Value {
			changed = append(changed, f)
		}
	}
	st.SetFacts(after)
	gate.Forget(st)
	if err := store.Save(st); err != nil {
		fmt.Fprintln(os.Stderr, "assume-nothing: could not save the probe record:", err)
	}
	if len(changed) == 0 {
		return nil
	}
	return withContext("PostToolUse", "assume-nothing: the environment changed after that install:\n"+render.Facts(changed))
}

func deny(reason string) *Output {
	return &Output{&Specific{HookEventName: "PreToolUse", PermissionDecision: "deny", PermissionDecisionReason: reason}}
}

func withContext(event, text string) *Output {
	return &Output{&Specific{HookEventName: event, AdditionalContext: text}}
}

// Run reads one event from data and returns the answer for it.
func Run(event string, data []byte) (*Output, error) {
	var in Input
	if len(strings.TrimSpace(string(data))) > 0 {
		if err := json.Unmarshal(data, &in); err != nil {
			return nil, fmt.Errorf("bad hook input: %w", err)
		}
	}
	if in.SessionID == "" {
		in.SessionID = "default"
	}
	switch event {
	case "session-start", "SessionStart":
		return SessionStart(in), nil
	case "pre-tool-use", "PreToolUse":
		return PreToolUse(in), nil
	case "post-tool-use", "PostToolUse":
		return PostToolUse(in), nil
	}
	return nil, fmt.Errorf("unknown hook event %q", event)
}
