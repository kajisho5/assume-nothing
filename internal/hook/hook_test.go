package hook

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kajisho5/assume-nothing/internal/gate"
	"github.com/kajisho5/assume-nothing/internal/store"
)

// fictional project used by every test
func project(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "nebula-widgets")
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"nebula-widgets"}`), 0o644)
	os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# fictional instructions"), 0o644)
	return dir
}

func setup(t *testing.T) {
	t.Helper()
	t.Setenv("ASSUME_NOTHING_DATA", t.TempDir())
	t.Setenv("ASSUME_NOTHING_STRICT", "")
	t.Setenv("CLAUDE_PLUGIN_OPTION_STRICT", "")
}

func event(t *testing.T, name string, in map[string]any) *Output {
	t.Helper()
	b, _ := json.Marshal(in)
	out, err := Run(name, b)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestSessionStartInjectsSnapshot(t *testing.T) {
	setup(t)
	dir := project(t)
	out := event(t, "session-start", map[string]any{"session_id": "abc", "cwd": dir, "source": "startup"})
	if out == nil || out.HookSpecificOutput == nil {
		t.Fatal("no output")
	}
	ctx := out.HookSpecificOutput.AdditionalContext
	for _, want := range []string{"CONFIRMED", "Node (package.json)", "AGENTS.md", "RULES", "Do not assume"} {
		if !strings.Contains(ctx, want) {
			t.Errorf("snapshot lacks %q:\n%s", want, ctx)
		}
	}
	if len([]rune(ctx)) > 2500 {
		t.Errorf("snapshot too long: %d", len([]rune(ctx)))
	}
	if out.HookSpecificOutput.PermissionDecision != "" {
		t.Error("session start must not set a permission decision")
	}
}

func TestPreToolUseBlocksUntilProbed(t *testing.T) {
	setup(t)
	dir := project(t)
	in := map[string]any{"session_id": "fresh", "cwd": dir, "tool_name": "Read", "tool_input": map[string]any{"file_path": dir + "/package.json"}}
	out := event(t, "pre-tool-use", in)
	if out == nil || out.HookSpecificOutput.PermissionDecision != "deny" {
		t.Fatalf("first call without a probe should be denied, got %+v", out)
	}
	if !strings.Contains(out.HookSpecificOutput.PermissionDecisionReason, "CONFIRMED") {
		t.Error("deny reason should carry the snapshot")
	}
	if out := event(t, "pre-tool-use", in); out != nil {
		t.Fatalf("second call should pass silently, got %+v", out.HookSpecificOutput)
	}
}

func TestNeverAnswersAllow(t *testing.T) {
	setup(t)
	dir := project(t)
	event(t, "session-start", map[string]any{"session_id": "s", "cwd": dir})
	for _, cmd := range []string{"ls", "npm test", "zzz-not-a-real-tool-42", "zzz-not-a-real-tool-42"} {
		out := event(t, "pre-tool-use", map[string]any{"session_id": "s", "cwd": dir, "tool_name": "Bash", "tool_input": map[string]any{"command": cmd}})
		if out != nil && out.HookSpecificOutput.PermissionDecision == "allow" {
			t.Fatalf("hook must never answer allow (%q)", cmd)
		}
	}
}

func TestMissingCommandStopOnce(t *testing.T) {
	setup(t)
	dir := project(t)
	old := gate.Lookup
	defer func() { gate.Lookup = old }()
	gate.Lookup = func(name string) store.Fact {
		if name == "npm" {
			return store.Fact{Key: "cmd.npm", Status: store.OK}
		}
		return store.Fact{Key: "cmd." + name, Status: store.Missing}
	}
	event(t, "session-start", map[string]any{"session_id": "m", "cwd": dir})
	bash := func(cmd string) *Output {
		return event(t, "pre-tool-use", map[string]any{"session_id": "m", "cwd": dir, "tool_name": "Bash", "tool_input": map[string]any{"command": cmd}})
	}
	if out := bash("npm test"); out != nil {
		t.Fatalf("installed command blocked: %+v", out.HookSpecificOutput)
	}
	out := bash("zorbtool render scene.zorb")
	if out == nil || out.HookSpecificOutput.PermissionDecision != "deny" || !strings.Contains(out.HookSpecificOutput.PermissionDecisionReason, "zorbtool") {
		t.Fatalf("missing command not blocked: %+v", out)
	}
	if out := bash("zorbtool render scene.zorb"); out != nil {
		t.Fatalf("second attempt should go through: %+v", out.HookSpecificOutput)
	}
}

func TestSelfCallsPassEvenWithoutProbe(t *testing.T) {
	setup(t)
	dir := project(t)
	out := event(t, "pre-tool-use", map[string]any{"session_id": "self", "cwd": dir, "tool_name": "Bash", "tool_input": map[string]any{"command": `"/x/assume-nothing/scripts/run.sh" deep net`}})
	if out != nil {
		t.Fatalf("self call blocked: %+v", out.HookSpecificOutput)
	}
}

func TestFolderChangeAddsContext(t *testing.T) {
	setup(t)
	a, b := project(t), project(t)
	event(t, "session-start", map[string]any{"session_id": "cd", "cwd": a})
	out := event(t, "pre-tool-use", map[string]any{"session_id": "cd", "cwd": b, "tool_name": "Read", "tool_input": map[string]any{}})
	if out == nil || !strings.Contains(out.HookSpecificOutput.AdditionalContext, "working folder changed") {
		t.Fatalf("folder change not reported: %+v", out)
	}
	if out.HookSpecificOutput.PermissionDecision != "" {
		t.Fatal("folder change must not block")
	}
}

func TestPostToolUseIgnoresNonInstall(t *testing.T) {
	setup(t)
	dir := project(t)
	event(t, "session-start", map[string]any{"session_id": "p", "cwd": dir})
	if out := event(t, "post-tool-use", map[string]any{"session_id": "p", "cwd": dir, "tool_name": "Bash", "tool_input": map[string]any{"command": "npm test"}}); out != nil {
		t.Fatalf("non-install command produced output: %+v", out.HookSpecificOutput)
	}
}

func TestSaveFailureNeverBlocksForever(t *testing.T) {
	setup(t)
	dir := project(t)
	event(t, "session-start", map[string]any{"session_id": "full", "cwd": dir})
	old := save
	defer func() { save = old }()
	calls := 0
	save = func(*store.State) error { calls++; return errors.New("disk full (simulated)") }
	in := map[string]any{"session_id": "full", "cwd": dir, "tool_name": "Bash", "tool_input": map[string]any{"command": "zz-fictional-missing --go"}}
	for i := 0; i < 3; i++ {
		out := event(t, "pre-tool-use", in)
		if out != nil && out.HookSpecificOutput.PermissionDecision == "deny" {
			t.Fatalf("attempt %d denied although the block cannot be recorded", i+1)
		}
		if out == nil || !strings.Contains(out.HookSpecificOutput.AdditionalContext, "not installed") {
			t.Fatalf("attempt %d: the warning should still reach the agent: %+v", i+1, out)
		}
	}
	// a session with no record and no way to save passes quietly, without probing
	calls = 0
	start := time.Now()
	if out := event(t, "pre-tool-use", map[string]any{"session_id": "never-saved", "cwd": dir, "tool_name": "Read", "tool_input": map[string]any{}}); out != nil {
		t.Fatalf("unsaveable fresh session should pass quietly: %+v", out.HookSpecificOutput)
	}
	if calls != 1 || time.Since(start) > 500*time.Millisecond {
		t.Fatalf("should fail fast without probing (saves=%d, took %v)", calls, time.Since(start))
	}
	// strict mode keeps blocking
	t.Setenv("ASSUME_NOTHING_STRICT", "1")
	if out := event(t, "pre-tool-use", map[string]any{"session_id": "never-saved-2", "cwd": dir, "tool_name": "Read", "tool_input": map[string]any{}}); out == nil || out.HookSpecificOutput.PermissionDecision != "deny" {
		t.Fatalf("strict mode should block: %+v", out)
	}
}

func TestSelfCallDetection(t *testing.T) {
	for cmd, want := range map[string]bool{
		`"/x/plugins/assume-nothing/scripts/run.sh" deep net`: true,
		`assume-nothing-darwin-arm64 probe --json`:            true,
		`cd ~/code/assume-nothing && npm test`:                false,
		`grep -r assume-nothing .`:                            false,
		`zzq; ./scripts/run.sh probe`:                         false,
		`./scripts/run.sh probe`:                              false,
		`"/x/assume-nothing/scripts/run.sh" deep net; zzq`:    false,
		`"/x/assume-nothing/scripts/run.sh" deep tool $(zzq)`: false,
	} {
		if got := callsSelf(cmd); got != want {
			t.Errorf("callsSelf(%q) = %v, want %v", cmd, got, want)
		}
	}
}

func TestBadInputIsAnError(t *testing.T) {
	if _, err := Run("pre-tool-use", []byte("{not json")); err == nil {
		t.Fatal("expected an error")
	}
	if _, err := Run("nope", []byte("{}")); err == nil {
		t.Fatal("expected an error for unknown event")
	}
}

func TestGeminiShapes(t *testing.T) {
	setup(t)
	dir := project(t)
	old := gate.Lookup
	defer func() { gate.Lookup = old }()
	gate.Lookup = func(name string) store.Fact { return store.Fact{Key: "cmd." + name, Status: store.Missing} }

	start := event(t, "SessionStart", map[string]any{"session_id": "g1", "cwd": dir, "source": "startup"})
	b, _ := Encode(start, Gemini)
	if !strings.Contains(string(b), `"additionalContext"`) || !strings.Contains(string(b), `"hookEventName":"SessionStart"`) {
		t.Fatalf("gemini session start: %s", b)
	}
	deny := event(t, "BeforeTool", map[string]any{"session_id": "g1", "cwd": dir, "tool_name": "run_shell_command", "tool_input": map[string]any{"command": "zz-fictional --go", "dir_path": "."}})
	b, _ = Encode(deny, Gemini)
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil || m["decision"] != "deny" || !strings.Contains(m["reason"].(string), "zz-fictional") {
		t.Fatalf("gemini deny: %s", b)
	}
	if _, ok := m["hookSpecificOutput"]; ok {
		t.Fatalf("gemini deny must use the top-level decision only: %s", b)
	}
	// a BeforeTool answer that only carries context is dropped (Gemini has no field for it)
	ctxOnly := &Output{HookSpecificOutput: &Specific{HookEventName: "PreToolUse", AdditionalContext: "x"}}
	if b, _ := Encode(ctxOnly, Gemini); b != nil {
		t.Fatalf("expected nothing, got %s", b)
	}
	// Codex gets the Claude shape unchanged
	if b, _ := Encode(deny, Codex); !strings.Contains(string(b), `"permissionDecision":"deny"`) {
		t.Fatalf("codex deny: %s", b)
	}
}

func TestTargetLookupFlow(t *testing.T) {
	setup(t)
	dir := project(t)
	event(t, "session-start", map[string]any{"session_id": "tg", "cwd": dir})

	// The request names a target: the agent is told it is UNKNOWN.
	out := event(t, "user-prompt-submit", map[string]any{"session_id": "tg", "cwd": dir, "prompt": "lumen-kitについて調べて"})
	if out == nil || !strings.Contains(out.HookSpecificOutput.AdditionalContext, "lumen-kit") {
		t.Fatalf("no target note: %+v", out)
	}
	// Answering about it without any lookup is sent back once.
	stop := map[string]any{"session_id": "tg", "cwd": dir, "last_assistant_message": "lumen-kit is a UI library with 40 components."}
	out = event(t, "stop", stop)
	if out == nil || out.Decision != "block" || !strings.Contains(out.Reason, "lumen-kit") {
		t.Fatalf("expected a block: %+v", out)
	}
	b, _ := Encode(out, Claude)
	if !strings.Contains(string(b), `"hookEventName":"Stop"`) || !strings.Contains(string(b), `"additionalContext"`) {
		t.Fatalf("claude shape: %s", b)
	}
	if c, _ := Encode(out, Codex); !strings.Contains(string(c), `"decision":"block"`) {
		t.Fatalf("codex shape: %s", c)
	}
	g, _ := Encode(out, Gemini)
	if !strings.Contains(string(g), `"decision":"deny"`) {
		t.Fatalf("gemini shape: %s", g)
	}
	// Once only, even if the agent still did not look it up.
	if out := event(t, "stop", stop); out != nil {
		t.Fatalf("blocked twice: %+v", out)
	}
	stop["stop_hook_active"] = true
	if out := event(t, "stop", stop); out != nil {
		t.Fatalf("blocked while stop_hook_active: %+v", out)
	}
}

func TestTargetLookedUpPasses(t *testing.T) {
	setup(t)
	dir := project(t)
	event(t, "session-start", map[string]any{"session_id": "tl", "cwd": dir})
	event(t, "user-prompt-submit", map[string]any{"session_id": "tl", "cwd": dir, "prompt": "orbit-labs/quasar-cli の使い方は？"})
	// A write that mentions it does not count as a lookup.
	event(t, "post-tool-use", map[string]any{"session_id": "tl", "cwd": dir, "tool_name": "Write",
		"tool_input": map[string]any{"file_path": "/tmp/quasar-cli.md", "content": "orbit-labs/quasar-cli"}})
	stop := map[string]any{"session_id": "tl", "cwd": dir, "last_assistant_message": "orbit-labs/quasar-cli has a build command."}
	// A fetch of its page does.
	event(t, "post-tool-use", map[string]any{"session_id": "tl", "cwd": dir, "tool_name": "WebFetch",
		"tool_input": map[string]any{"url": "https://github.com/orbit-labs/quasar-cli"}, "tool_response": "quasar-cli: a fictional build tool. Run quasar build to compile."})
	if out := event(t, "stop", stop); out != nil {
		t.Fatalf("blocked after a lookup: %+v", out)
	}
	// An answer that already says it is not verified is not sent back.
	event(t, "user-prompt-submit", map[string]any{"session_id": "tl", "prompt": "zentrova-cliとは"})
	if out := event(t, "stop", map[string]any{"session_id": "tl", "last_assistant_message": "zentrova-cli については確かな情報がありません。未確認です。"}); out != nil {
		t.Fatalf("blocked a hedged answer: %+v", out)
	}
	// A long answer to the latest request is about it even without the name.
	event(t, "user-prompt-submit", map[string]any{"session_id": "tl", "prompt": "nebula-dockの使い方"})
	event(t, "user-prompt-submit", map[string]any{"session_id": "tl", "prompt": "nebula-dockの使い方、詳しく"})
	long := strings.Repeat("Run the build command, then open the dashboard and pick a profile. ", 3)
	if out := event(t, "stop", map[string]any{"session_id": "tl", "last_assistant_message": long}); out == nil || out.Decision != "block" {
		t.Fatalf("long unlooked answer not sent back: %+v", out)
	}
	// A later request that names nothing is not checked against it.
	event(t, "user-prompt-submit", map[string]any{"session_id": "tl", "prompt": "thanks, now tidy up"})
	if out := event(t, "stop", map[string]any{"session_id": "tl", "last_assistant_message": long}); out != nil {
		t.Fatalf("checked against an older request: %+v", out)
	}
	// An answer that does not mention it is never stopped.
	event(t, "user-prompt-submit", map[string]any{"session_id": "tl", "cwd": dir, "prompt": "and nebula-port?"})
	if out := event(t, "stop", map[string]any{"session_id": "tl", "last_assistant_message": "I don't know."}); out != nil {
		t.Fatalf("blocked an answer that does not mention the target: %+v", out)
	}
}

func TestPromptSubmitProbesWhenSessionStartWasMissed(t *testing.T) {
	setup(t)
	dir := project(t)
	out := event(t, "user-prompt-submit", map[string]any{"session_id": "late", "cwd": dir, "prompt": "hello"})
	if out == nil || !strings.Contains(out.HookSpecificOutput.AdditionalContext, "CONFIRMED") {
		t.Fatalf("no probe on first prompt: %+v", out)
	}
	// The first tool call then goes through instead of being stopped.
	if out := event(t, "pre-tool-use", map[string]any{"session_id": "late", "cwd": dir, "tool_name": "Read", "tool_input": map[string]any{"file_path": "x"}}); out != nil && out.HookSpecificOutput.PermissionDecision == "deny" {
		t.Fatalf("first tool still denied: %+v", out)
	}
	g, _ := Encode(event(t, "user-prompt-submit", map[string]any{"session_id": "late2", "cwd": dir, "prompt": "hi"}), Gemini)
	if !strings.Contains(string(g), `"BeforeAgent"`) {
		t.Fatalf("gemini BeforeAgent shape: %s", g)
	}
}

func TestTargetsOff(t *testing.T) {
	setup(t)
	t.Setenv("ASSUME_NOTHING_TARGETS", "0")
	dir := project(t)
	event(t, "session-start", map[string]any{"session_id": "off", "cwd": dir})
	if out := event(t, "user-prompt-submit", map[string]any{"session_id": "off", "prompt": "lumen-kitについて"}); out != nil {
		t.Fatalf("named targets while off: %+v", out)
	}
	if out := event(t, "stop", map[string]any{"session_id": "off", "last_assistant_message": "lumen-kit is great"}); out != nil {
		t.Fatalf("stopped while off: %+v", out)
	}
}

func TestDetectAgent(t *testing.T) {
	t.Setenv("CLAUDE_PROJECT_DIR", "/work/nebula")
	if a := DetectAgent([]byte(`{"session_id":"x"}`), Claude); a != Claude {
		t.Errorf("claude seen as %s", a)
	}
	if a := DetectAgent([]byte(`{"turn_id":"t1"}`), Claude); a != Codex {
		t.Errorf("codex input seen as %s", a)
	}
	if a := DetectAgent([]byte(`{}`), Gemini); a != Gemini {
		t.Errorf("gemini changed to %s", a)
	}
	t.Setenv("CLAUDE_PROJECT_DIR", "")
	if a := DetectAgent([]byte(`{}`), Claude); a != Codex {
		t.Errorf("without CLAUDE_PROJECT_DIR expected the codex shape, got %s", a)
	}
}

func TestStrictLookups(t *testing.T) {
	setup(t)
	dir := project(t)
	sess := func(id string) {
		event(t, "session-start", map[string]any{"session_id": id, "cwd": dir})
	}
	post := func(id, tool string, input map[string]any, resp string) {
		event(t, "post-tool-use", map[string]any{"session_id": id, "tool_name": tool, "tool_input": input, "tool_response": resp})
	}
	stop := func(id, msg string) *Output {
		return event(t, "stop", map[string]any{"session_id": id, "last_assistant_message": msg})
	}

	// A fetch that failed is not a lookup.
	sess("s1")
	event(t, "user-prompt-submit", map[string]any{"session_id": "s1", "prompt": "lumen-kitについて調べて"})
	post("s1", "WebFetch", map[string]any{"url": "https://example.com/lumen-kit"}, "Request failed with status code 403")
	if out := stop("s1", "lumen-kit has 40 widgets."); out == nil {
		t.Fatal("a failed fetch counted as a lookup")
	}

	// A subagent's report is not a lookup by the main agent.
	sess("s2")
	event(t, "user-prompt-submit", map[string]any{"session_id": "s2", "prompt": "lumen-kitについて調べて"})
	post("s2", "Agent", map[string]any{"prompt": "research lumen-kit"}, "lumen-kit is a fictional widget toolkit with 40 widgets.")
	if out := stop("s2", "lumen-kit has 40 widgets."); out == nil {
		t.Fatal("an Agent call counted as a lookup")
	}

	// A hedge far from the name does not cover it.
	sess("s3")
	event(t, "user-prompt-submit", map[string]any{"session_id": "s3", "prompt": "lumen-kitについて"})
	if out := stop("s3", "lumen-kit 3.2 has themes. It ships 40 widgets. It runs on Node 22. It is MIT licensed. The cache layer is unverified."); out == nil {
		t.Fatal("a distant hedge let the answer through")
	}

	// A question about the latest version needs a lookup, even with no name.
	sess("s4")
	event(t, "user-prompt-submit", map[string]any{"session_id": "s4", "prompt": "このライブラリの最新版を教えて"})
	long := "The latest version is 9.1, released last month with a new renderer, faster builds, a smaller bundle size overall, and better error messages for broken configuration files."
	out := stop("s4", long)
	if out == nil || !strings.Contains(out.Reason, "changes over time") {
		t.Fatalf("fresh question answered from memory not sent back: %+v", out)
	}
	if out := stop("s4", long); out != nil {
		t.Fatal("fresh question sent back twice")
	}
	sess("s5")
	event(t, "user-prompt-submit", map[string]any{"session_id": "s5", "prompt": "このライブラリの最新版を教えて"})
	post("s5", "Bash", map[string]any{"command": "npm view nebula-widgets version"}, `{"stdout":"9.1.0 (a fictional package published for this test suite)","stderr":""}`)
	if out := stop("s5", long); out != nil {
		t.Fatalf("fresh question with a lookup sent back: %+v", out)
	}
}

func TestFreshHedgeStillSentBackOnce(t *testing.T) {
	setup(t)
	dir := project(t)
	event(t, "session-start", map[string]any{"session_id": "fh", "cwd": dir})
	event(t, "user-prompt-submit", map[string]any{"session_id": "fh", "prompt": "Zephyrdocの最新の安定版は？"})
	msg := "Zephyrdoc の最新安定版は 2.3 です（記憶に基づく回答で、検証はしていません）。"
	if out := event(t, "stop", map[string]any{"session_id": "fh", "last_assistant_message": msg}); out == nil {
		t.Fatal("a hedged memory answer to a latest-version question was not sent back")
	}
	if out := event(t, "stop", map[string]any{"session_id": "fh", "last_assistant_message": msg}); out != nil {
		t.Fatal("sent back twice")
	}
	// Outside a time-sensitive question, the same hedge lets it through.
	event(t, "user-prompt-submit", map[string]any{"session_id": "fh", "prompt": "Quillmarkについて教えて"})
	if out := event(t, "stop", map[string]any{"session_id": "fh", "last_assistant_message": "Quillmark はエディタです（記憶に基づく回答で、検証はしていません）。"}); out != nil {
		t.Fatalf("hedged answer sent back: %+v", out)
	}
}

func TestSubagentStopAndNotice(t *testing.T) {
	setup(t)
	t.Setenv("CLAUDE_PROJECT_DIR", "/work/nebula")
	dir := project(t)
	event(t, "session-start", map[string]any{"session_id": "sa", "cwd": dir})
	event(t, "user-prompt-submit", map[string]any{"session_id": "sa", "prompt": "lumen-kitについて調べて"})
	sub := map[string]any{"session_id": "sa", "agent_id": "a1", "agent_type": "Explore", "last_assistant_message": "lumen-kit has 40 widgets."}
	out := event(t, "subagent-stop", sub)
	if out == nil {
		t.Fatal("subagent answer without a lookup not sent back")
	}
	b, _ := Encode(out, Claude)
	for _, want := range []string{`"hookEventName":"SubagentStop"`, `"systemMessage":"assume-nothing: sent a subagent's answer back once`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("encoded lacks %s: %s", want, b)
		}
	}
	// Claude Code's internal agents have no type and are left alone.
	if out := event(t, "subagent-stop", map[string]any{"session_id": "sa", "last_assistant_message": "lumen-kit has 40 widgets."}); out != nil {
		t.Fatal("internal agent checked")
	}
	// The subagent's send-back does not use up the main agent's.
	if out := event(t, "stop", map[string]any{"session_id": "sa", "last_assistant_message": "lumen-kit has 40 widgets."}); out == nil {
		t.Fatal("main agent not checked after a subagent send-back")
	}
	s, err := Status("sa")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"lumen-kit: NOT looked up", "answers sent back: 2", "requests: 1"} {
		if !strings.Contains(s, want) {
			t.Errorf("status lacks %q:\n%s", want, s)
		}
	}
}

func TestParallelFolderChangeNotedOnce(t *testing.T) {
	setup(t)
	dir := project(t)
	event(t, "session-start", map[string]any{"session_id": "pc", "cwd": dir})
	other := project(t)
	var mu sync.Mutex
	notes := 0
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b, _ := json.Marshal(map[string]any{"session_id": "pc", "cwd": other, "tool_name": "Read", "tool_input": map[string]any{}})
			out, _ := Run("pre-tool-use", b)
			if out != nil && strings.Contains(out.HookSpecificOutput.AdditionalContext, "working folder changed") {
				mu.Lock()
				notes++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if notes != 1 {
		t.Fatalf("folder change noted %d times, want 1", notes)
	}
}
