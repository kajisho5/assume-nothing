// Package hook answers the lifecycle hooks of coding agents.
//
// The input and output shapes follow Claude Code's hooks (SessionStart,
// UserPromptSubmit, PreToolUse, PostToolUse, Stop). Codex CLI uses the same
// field names for these events; Gemini CLI is converted in Encode.
package hook

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/kajisho5/assume-nothing/internal/gate"
	"github.com/kajisho5/assume-nothing/internal/probe"
	"github.com/kajisho5/assume-nothing/internal/render"
	"github.com/kajisho5/assume-nothing/internal/store"
	"github.com/kajisho5/assume-nothing/internal/target"
)

// Input is what the agent sends on stdin.
type Input struct {
	SessionID     string          `json:"session_id"`
	Cwd           string          `json:"cwd"`
	HookEventName string          `json:"hook_event_name"`
	Source        string          `json:"source"`
	ToolName      string          `json:"tool_name"`
	ToolInput     json.RawMessage `json:"tool_input"`
	ToolResponse  json.RawMessage `json:"tool_response"`
	Prompt        string          `json:"prompt"`           // UserPromptSubmit, BeforeAgent
	StopActive    bool            `json:"stop_hook_active"` // Stop, AfterAgent
	LastMessage   string          `json:"last_assistant_message"`
	PromptResp    string          `json:"prompt_response"` // Gemini's AfterAgent
	AgentID       string          `json:"agent_id"`        // SubagentStop
	AgentType     string          `json:"agent_type"`      // SubagentStop

	// Cursor names some of these differently.
	ConversationID string          `json:"conversation_id"`
	WorkspaceRoots []string        `json:"workspace_roots"`
	ToolOutput     json.RawMessage `json:"tool_output"`
	LoopCount      int             `json:"loop_count"`
	Text           string          `json:"text"` // afterAgentResponse
	CursorVersion  string          `json:"cursor_version"`
}

// normalize fills the Claude Code field names from other agents' names.
func (in *Input) normalize() {
	if in.SessionID == "" {
		in.SessionID = in.ConversationID
	}
	if in.Cwd == "" && len(in.WorkspaceRoots) > 0 {
		in.Cwd = in.WorkspaceRoots[0]
	}
	if len(in.ToolResponse) == 0 && len(in.ToolOutput) > 0 {
		in.ToolResponse = in.ToolOutput
	}
	if in.LoopCount > 0 {
		in.StopActive = true // Cursor: this stop already sent a follow-up
	}
}

// Output is what the hook prints on stdout. Nil means "say nothing".
type Output struct {
	Decision           string    `json:"decision,omitempty"` // only "block", only for Stop
	Reason             string    `json:"reason,omitempty"`
	HookSpecificOutput *Specific `json:"hookSpecificOutput,omitempty"`
	Event              string    `json:"-"` // "Stop" or "SubagentStop" for a send-back
	Notice             string    `json:"-"` // one line for the user, where the agent can show it
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

// save writes the session record; tests replace it to simulate a full disk.
var save = store.Save

// Strict reports whether a broken probe should keep blocking tools.
func Strict() bool {
	switch strings.ToLower(os.Getenv("ASSUME_NOTHING_STRICT")) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// TargetsOff reports whether the lookup check is turned off
// (ASSUME_NOTHING_TARGETS=0). The environment probe still runs.
func TargetsOff() bool {
	switch strings.ToLower(os.Getenv("ASSUME_NOTHING_TARGETS")) {
	case "0", "false", "no", "off":
		return true
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
	if err := save(st); err != nil {
		fmt.Fprintln(os.Stderr, "assume-nothing: could not save the probe record:", err)
	}
	store.Prune(30 * 24 * time.Hour)
	return &Output{HookSpecificOutput: &Specific{HookEventName: "SessionStart", AdditionalContext: render.Snapshot(st, DeepCommand)}}
}

// shellTools are the shell tool names of the supported agents:
// Claude Code and Codex ("Bash"), Gemini CLI ("run_shell_command").
var shellTools = map[string]bool{"Bash": true, "run_shell_command": true, "Shell": true, "bash": true}

func bashCommand(in Input) string {
	if !shellTools[in.ToolName] {
		return ""
	}
	var ti struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal(in.ToolInput, &ti)
	return ti.Command
}

// selfCall matches a line that is nothing but this tool's own check, e.g.
//
//	"/home/me/.claude/plugins/.../assume-nothing/.../scripts/run.sh" deep net
//	assume-nothing-linux-amd64 probe --json
//
// The whole line must be that one command (no ; & | ` $ ( ) < > or newline),
// and its path must contain "assume-nothing".
var selfCall = regexp.MustCompile(`^\s*["']?[^;&|` + "`" + `$()<>\n"']*assume-nothing[^;&|` + "`" + `$()<>\n"' ]*["']?\s+(deep|probe|status|version|help)\b[^;&|` + "`" + `$()<>\n]*$`)

// Clean is a short alias used in messages.
func Clean(s string) string { return probe.Clean(s, 120) }

func callsSelf(cmd string) bool {
	return selfCall.MatchString(cmd)
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
		// Check that a record can be saved at all before spending time probing.
		if serr := save(store.New(in.SessionID, cwdOf(in))); serr != nil {
			fmt.Fprintln(os.Stderr, "assume-nothing: could not save the probe record:", serr)
			if Strict() {
				return deny("assume-nothing: the probe record cannot be saved, and strict mode is on, so tools stay blocked. Fix the plugin data folder (" + Clean(serr.Error()) + ") or turn strict mode off.")
			}
			// Without a record we cannot remember anything between calls, so
			// blocking could never end. Let the call through quietly; the
			// session-start snapshot already gave the agent the facts.
			return nil
		}
		st = fresh(in)
		snap := render.Snapshot(st, DeepCommand)
		if serr := save(st); serr != nil {
			fmt.Fprintln(os.Stderr, "assume-nothing: could not save the probe record:", serr)
			if !Strict() {
				return nil
			}
		}
		return deny("assume-nothing: this session had no environment probe yet, so one was just run. Read it, then make the same call again.\n\n" + snap)
	}

	var notes []string
	cwd := cwdOf(in)
	if cwd != "" && cwd != st.Cwd {
		ctx, cancel := context.WithTimeout(context.Background(), probe.Budget)
		tf := probe.TargetFacts(ctx, cwd)
		cancel()
		tf = append(tf, probe.VersionFacts(cwd, st.Facts)...)
		st.DropPrefix("ver.")
		st.SetFacts(tf)
		st.Cwd = cwd
		notes = append(notes, "assume-nothing: the working folder changed. New folder facts:\n"+render.Facts(tf))
	}

	var out *Output
	if cmd != "" {
		if missing := gate.MissingCommands(cmd, st); len(missing) > 0 {
			out = deny(missingReason(missing, st))
			out.Notice = "assume-nothing: stopped a command that is not installed: " + strings.Join(missing, ", ") + "."
		}
	}
	if err := save(st); err != nil {
		fmt.Fprintln(os.Stderr, "assume-nothing: could not save the probe record:", err)
		if out != nil && !Strict() {
			// The block could not be recorded, so a retry would be blocked
			// again forever. Warn instead of blocking.
			notes = append(notes, out.HookSpecificOutput.PermissionDecisionReason)
			out = nil
		}
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

// writeTools never count as looking something up: they write or talk.
var writeTools = map[string]bool{
	"Write": true, "Edit": true, "MultiEdit": true, "NotebookEdit": true, "TodoWrite": true,
	"TaskCreate": true, "TaskUpdate": true, "AskUserQuestion": true, "SendUserMessage": true,
	"SendUserFile": true, "apply_patch": true, "write_file": true, "replace": true,
	"write_todos": true, "save_memory": true,
	// Handing work to a subagent or loading instructions is not looking
	// something up; the subagent's own tool calls are checked on their own.
	"Agent": true, "Task": true, "SubagentHandback": true, "Skill": true, "ToolSearch": true,
	"TaskList": true, "TaskGet": true, "TaskStop": true, "EnterPlanMode": true, "ExitPlanMode": true,
	// Cursor and OpenCode
	"Delete": true, "write": true, "edit": true, "patch": true, "todowrite": true, "todoread": true, "task": true,
}

// webTools fetch or search the web.
var webTools = map[string]bool{"WebFetch": true, "WebSearch": true, "web_fetch": true, "google_web_search": true, "webfetch": true, "websearch": true}

// netCommand matches a shell command that reaches the network to look something up.
var netCommand = regexp.MustCompile(`https?://|(?:^|[\s;&|(])(?:curl|wget|gh|http|xh|git\s+(?:ls-remote|fetch|clone)|npm\s+(?:view|info|show|search)|pnpm\s+view|yarn\s+info|pip3?\s+(?:index|download)|uv\s+pip\s+index|cargo\s+(?:search|info)|go\s+list\s+-m|brew\s+info)\b`)

// isLookup reports whether a tool call looks something up outside the
// machine: the web, an MCP server, or a network command.
func isLookup(in Input) bool {
	if webTools[in.ToolName] || strings.HasPrefix(in.ToolName, "mcp__") || strings.HasPrefix(in.ToolName, "mcp_") || strings.HasPrefix(in.ToolName, "MCP:") {
		return true
	}
	return netCommand.MatchString(bashCommand(in))
}

func targetLog(session string) (target.Log, bool) {
	p, err := store.TargetLogPath(session)
	if err != nil {
		return target.Log{}, false
	}
	return target.Log{Path: p}, true
}

// noteLookup records which named targets a finished tool call looked at.
func noteLookup(in Input) {
	if writeTools[in.ToolName] || in.ToolName == "" {
		return
	}
	lg, ok := targetLog(in.SessionID)
	if !ok {
		return
	}
	ts, err := lg.Read()
	if err != nil {
		return
	}
	if keys := ts.Touched(in.ToolInput, in.ToolResponse); len(keys) > 0 {
		if err := lg.MarkSeen(keys, in.ToolName); err != nil {
			fmt.Fprintln(os.Stderr, "assume-nothing: could not save the lookup record:", err)
		}
	}
	if ts.Fresh && !ts.Looked && isLookup(in) && !target.Failed(in.ToolResponse) {
		if err := lg.MarkLookup(in.ToolName); err != nil {
			fmt.Fprintln(os.Stderr, "assume-nothing: could not save the lookup record:", err)
		}
	}
}

// PostToolUse notes what a tool looked at, and re-checks tools after
// something was installed and reports what changed.
func PostToolUse(in Input) *Output {
	noteLookup(in)
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
	if err := save(st); err != nil {
		fmt.Fprintln(os.Stderr, "assume-nothing: could not save the probe record:", err)
	}
	if len(changed) == 0 {
		return nil
	}
	return withContext("PostToolUse", "assume-nothing: the environment changed after that install:\n"+render.Facts(changed))
}

// UserPromptSubmit runs before the agent reads a request. It makes sure a
// probe exists (so the first tool call is not stopped for it), and names the
// things the request is about, which stay UNKNOWN until looked up.
func UserPromptSubmit(in Input) *Output {
	var notes []string
	if _, err := store.Load(in.SessionID); err != nil {
		st := fresh(in)
		if serr := save(st); serr != nil {
			fmt.Fprintln(os.Stderr, "assume-nothing: could not save the probe record:", serr)
		}
		notes = append(notes, render.Snapshot(st, DeepCommand))
	}
	if !TargetsOff() {
		ts := target.Extract(in.Prompt)
		if lg, ok := targetLog(in.SessionID); ok {
			// A request that names nothing is still recorded once targets exist,
			// so the next answer is not checked against an older request.
			isFresh := target.Fresh(in.Prompt)
			if have, err := lg.Read(); err == nil && (len(ts) > 0 || len(have.Targets) > 0 || isFresh) {
				var open []target.Target
				for _, t := range ts {
					if _, seen := have.Seen[t.Key]; !seen {
						open = append(open, t)
					}
				}
				if err := lg.AddPrompt(ts, isFresh); err != nil {
					fmt.Fprintln(os.Stderr, "assume-nothing: could not save the target list:", err)
				} else if len(open) > 0 || isFresh {
					notes = append(notes, targetNote(open, isFresh))
				}
			}
		}
	}
	if len(notes) == 0 {
		return nil
	}
	return withContext("UserPromptSubmit", strings.Join(notes, "\n\n"))
}

func names(ts []target.Target) string {
	var n []string
	for _, t := range ts {
		n = append(n, Clean(t.Text))
	}
	return strings.Join(n, ", ")
}

func targetNote(ts []target.Target, fresh bool) string {
	var b strings.Builder
	b.WriteString("[assume-nothing: targets]\n")
	if len(ts) > 0 {
		b.WriteString("This request is about: " + names(ts) + ".\n" +
			"Facts about them are UNKNOWN until you look them up in this session, in a primary source " +
			"(the official docs, the repository, the file itself). What you remember may be outdated or wrong.\n")
	}
	if fresh {
		b.WriteString("It asks about something that changes over time (a latest version, a price). " +
			"Memory cannot answer that: look it up in this session.\n")
	}
	b.WriteString("An answer that states such facts without a lookup will be sent back once. " +
		"If you cannot look it up, say so next to the statement.")
	return b.String()
}

// SubstantialAnswer is the length (in characters) from which an answer counts
// as being about what the request named, even without repeating the name.
const SubstantialAnswer = 120

// Stop runs when the agent is about to finish. If its answer talks about a
// target that nothing in the session looked at, it is sent back once.
func Stop(in Input) *Output { return stopCheck(in, "", "Stop") }

// SubagentStop gives a subagent's answer the same check. Claude Code's own
// internal agents (no agent type) are left alone, and a subagent's send-back
// does not use up the main agent's.
func SubagentStop(in Input) *Output {
	if in.AgentType == "" {
		return nil
	}
	return stopCheck(in, "sub:"+in.AgentID+":", "SubagentStop")
}

func stopCheck(in Input, scope, event string) *Output {
	if in.StopActive || TargetsOff() {
		return nil // already sent back once (trust it now), or turned off
	}
	msg := in.LastMessage
	if msg == "" {
		msg = in.PromptResp
	}
	if msg == "" {
		msg = lastAnswer(in.SessionID)
	}
	if strings.TrimSpace(msg) == "" {
		return nil
	}
	lg, ok := targetLog(in.SessionID)
	if !ok {
		return nil
	}
	ts, err := lg.Read()
	if err != nil {
		return nil
	}
	// A real answer to the latest request is about what that request named,
	// even when it does not repeat the name.
	substantial := len([]rune(strings.TrimSpace(msg))) >= SubstantialAnswer
	latest, asked := map[string]bool{}, map[string]bool{}
	for _, k := range ts.Latest {
		asked[k] = true
		if substantial {
			latest[k] = true
		}
	}
	var hit []target.Target
	var keys []string
	for _, t := range ts.Unseen() {
		if ts.Warned[scope+t.Key] {
			continue
		}
		switch {
		case target.Mentions(msg, t):
			// Named in the answer: only a hedge next to the name covers it.
			// For a question about something that changes, a hedge is not
			// enough the first time: memory is the wrong source, so try a lookup.
			if target.HedgedAbout(msg, t) && !(ts.Fresh && asked[t.Key]) {
				continue
			}
		case latest[t.Key]:
			// An answer to the request that never names it: any hedge covers it.
			if target.Hedged(msg) && !ts.Fresh {
				continue
			}
		default:
			continue
		}
		hit = append(hit, t)
		keys = append(keys, scope+t.Key)
	}
	// A question about something that changes needs some lookup this turn.
	// Memory is the wrong source here, so a hedge does not excuse it the first time.
	freshMiss := ts.Fresh && !ts.Looked && !ts.Warned[scope+ts.FreshKey()] && (substantial || len(hit) > 0)
	if freshMiss {
		keys = append(keys, scope+ts.FreshKey())
	}
	if len(keys) == 0 {
		return nil
	}
	if err := lg.MarkWarned(keys); err != nil {
		fmt.Fprintln(os.Stderr, "assume-nothing: could not save the target list:", err)
		return nil // a stop that cannot be remembered might repeat; let it go
	}
	what := "your answer talks about " + names(hit) + ", but nothing in this session looked it up (no tool read a page, file, or command output about it)"
	if len(hit) == 0 {
		what = "the request asks about something that changes over time (a latest version, a price), but nothing looked it up since the request (no web fetch or search, MCP tool, or network command succeeded)"
	} else if freshMiss {
		what += "; the request also asks about something that changes over time, and nothing looked that up"
	}
	notice := "assume-nothing: sent the answer back once, because nothing looked up " + names(hit) + "."
	if len(hit) == 0 {
		notice = "assume-nothing: sent the answer back once, because a question about the latest version or price was answered without a lookup."
	}
	if event == "SubagentStop" {
		notice = strings.Replace(notice, "the answer", "a subagent's answer", 1)
	}
	return &Output{Event: event, Notice: notice, Decision: "block", Reason: "assume-nothing: " + what + ".\n" +
		"Before you finish, either look it up in a primary source (official docs, the repository, the file itself) " +
		"and correct the answer where it was wrong, or tell the user plainly which statements are not verified.\n" +
		"If you did check it with something this hook cannot see (for example a hosted web search), say where you read it, then finish."}
}

// AfterAgentResponse keeps the agent's latest answer for a stop hook that
// does not receive it (Cursor).
func AfterAgentResponse(in Input) *Output {
	if p, err := store.LastPath(in.SessionID); err == nil && strings.TrimSpace(in.Text) != "" {
		_ = os.MkdirAll(filepath.Dir(p), 0o700)
		_ = os.WriteFile(p, []byte(in.Text), 0o600)
	}
	return nil
}

func lastAnswer(session string) string {
	p, err := store.LastPath(session)
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	_ = os.Remove(p) // one answer, one check
	return string(b)
}

func deny(reason string) *Output {
	return &Output{HookSpecificOutput: &Specific{HookEventName: "PreToolUse", PermissionDecision: "deny", PermissionDecisionReason: reason}}
}

func withContext(event, text string) *Output {
	return &Output{HookSpecificOutput: &Specific{HookEventName: event, AdditionalContext: text}}
}

// Run reads one event from data and returns the answer for it.
func Run(event string, data []byte) (*Output, error) {
	var in Input
	if len(strings.TrimSpace(string(data))) > 0 {
		if err := json.Unmarshal(data, &in); err != nil {
			return nil, fmt.Errorf("bad hook input: %w", err)
		}
	}
	in.normalize()
	if in.SessionID == "" {
		in.SessionID = "default"
	}
	switch event {
	case "stop", "Stop", "AfterAgent", "subagent-stop", "SubagentStop", "subagentStop", "after-agent-response", "afterAgentResponse":
		// these only read and append to the target log
	default:
		// Parallel tool calls each run a hook process; one at a time per
		// session, so they do not overwrite each other's record.
		defer store.Lock(in.SessionID)()
	}
	switch event {
	case "session-start", "SessionStart", "sessionStart":
		return SessionStart(in), nil
	case "pre-tool-use", "PreToolUse", "BeforeTool", "preToolUse":
		return PreToolUse(in), nil
	case "post-tool-use", "PostToolUse", "AfterTool", "postToolUse":
		return PostToolUse(in), nil
	case "user-prompt-submit", "UserPromptSubmit", "BeforeAgent", "beforeSubmitPrompt":
		return UserPromptSubmit(in), nil
	case "stop", "Stop", "AfterAgent":
		return Stop(in), nil
	case "subagent-stop", "SubagentStop", "subagentStop":
		return SubagentStop(in), nil
	case "after-agent-response", "afterAgentResponse":
		return AfterAgentResponse(in), nil
	}
	return nil, fmt.Errorf("unknown hook event %q", event)
}

// Agents this hook can answer.
const (
	Claude = "claude" // Claude Code
	Codex  = "codex"  // Codex CLI: same input and output shape as Claude Code
	Gemini = "gemini" // Gemini CLI: BeforeTool / AfterTool, top-level decision
	Cursor = "cursor" // Cursor: flat permission / additional_context / followup_message
)

// DetectAgent refines the agent named on the command line. Codex reads the
// same hooks file as Claude Code, so both arrive as "claude"; Codex's input
// carries a turn_id and Claude Code sets CLAUDE_PROJECT_DIR. When unsure it
// answers Codex, whose shapes Claude Code also accepts.
func DetectAgent(data []byte, agent string) string {
	var probe struct {
		TurnID        string `json:"turn_id"`
		CursorVersion string `json:"cursor_version"`
	}
	_ = json.Unmarshal(data, &probe)
	if probe.CursorVersion != "" {
		return Cursor // also when Cursor runs hooks written for Claude Code
	}
	if agent != Claude {
		return agent
	}
	if probe.TurnID != "" || os.Getenv("CLAUDE_PROJECT_DIR") == "" {
		return Codex
	}
	return Claude
}

// Encode turns an answer into the JSON the given agent expects. It returns nil
// when there is nothing to say.
func Encode(out *Output, agent string) ([]byte, error) {
	if out == nil || (out.HookSpecificOutput == nil && out.Decision == "") {
		return nil, nil
	}
	if agent == Cursor {
		return encodeCursor(out)
	}
	if agent == Claude {
		// Claude Code continues the turn on Stop additionalContext too, and shows
		// it as hook feedback instead of a hook error. systemMessage shows the
		// user a one-line notice.
		ev := out.Event
		if ev == "" {
			ev = "Stop"
		}
		msg := map[string]any{}
		if out.Decision == "block" {
			msg["hookSpecificOutput"] = Specific{HookEventName: ev, AdditionalContext: out.Reason}
		} else {
			msg["hookSpecificOutput"] = out.HookSpecificOutput
		}
		if out.Notice != "" {
			msg["systemMessage"] = out.Notice
		}
		return json.Marshal(msg)
	}
	if agent != Gemini {
		return json.Marshal(out)
	}
	if out.Decision == "block" {
		// AfterAgent: "deny" rejects the answer and sends reason as a new prompt.
		return json.Marshal(map[string]string{"decision": "deny", "reason": out.Reason})
	}
	s := out.HookSpecificOutput
	if s == nil {
		return nil, nil
	}
	if s.PermissionDecision == "deny" {
		return json.Marshal(map[string]string{"decision": "deny", "reason": s.PermissionDecisionReason})
	}
	ev := map[string]string{"SessionStart": "SessionStart", "PostToolUse": "AfterTool", "UserPromptSubmit": "BeforeAgent"}[s.HookEventName]
	if ev == "" || s.AdditionalContext == "" {
		// Gemini's BeforeTool cannot add context; say nothing rather than guess.
		return nil, nil
	}
	return json.Marshal(map[string]any{"hookSpecificOutput": map[string]string{"hookEventName": ev, "additionalContext": s.AdditionalContext}})
}

// encodeCursor answers in Cursor's own hook format.
func encodeCursor(out *Output) ([]byte, error) {
	if out.Decision == "block" {
		// stop / subagentStop: the follow-up is sent as the next message.
		return json.Marshal(map[string]string{"followup_message": out.Reason})
	}
	s := out.HookSpecificOutput
	if s == nil {
		return nil, nil
	}
	if s.PermissionDecision == "deny" {
		m := map[string]string{"permission": "deny", "agent_message": s.PermissionDecisionReason}
		if out.Notice != "" {
			m["user_message"] = out.Notice
		}
		return json.Marshal(m)
	}
	switch s.HookEventName {
	case "SessionStart", "PostToolUse":
		if s.AdditionalContext != "" {
			return json.Marshal(map[string]string{"additional_context": s.AdditionalContext})
		}
	}
	// Cursor's preToolUse and beforeSubmitPrompt cannot add context; say nothing.
	return nil, nil
}
