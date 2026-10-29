package hook

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
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
