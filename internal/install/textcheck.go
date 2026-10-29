package install

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// textCheckMarker identifies the hook so it can be found and removed.
const textCheckMarker = "assume-nothing text check"

// TextCheckPrompt is the optional Stop hook prompt. A small model reads the
// final answer and flags concrete facts stated as certain with no source.
// It complements the behavior check, which only knows whether a named thing
// was looked at.
const TextCheckPrompt = `You are the ` + textCheckMarker + `. You review the final answer of a coding agent.
Hook input: $ARGUMENTS

If stop_hook_active is true, answer {"ok": true}.
Read only last_assistant_message. Flag a statement only if ALL of these hold:
1. It is a concrete, checkable fact that can change or be misremembered: a version number, a price or limit, a default value, a command-line flag or option name, an API or function name or signature, a release date, or what a specific file contains.
2. It is stated as certain, with no hedge such as "not verified", "from memory", "I think", "probably".
3. The answer gives no source for it: no file path it read, no command output, no URL, no "I checked".
Never flag opinions, plans, questions, code the agent itself just wrote, or stable general knowledge (how a language or protocol works).
If nothing is flagged, answer {"ok": true}.
Otherwise answer {"ok": false, "reason": "` + textCheckMarker + `: these are stated as certain without a source: <quote up to 3, each under 15 words>. Look them up in the project or the official source and correct them, or mark them as not verified."}`

// TextCheck adds (or removes) the optional text-check Stop hook in the
// Claude Code settings file at path.
func TextCheck(path string, remove bool) error {
	settings := map[string]any{}
	if b, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(b, &settings); err != nil {
			return fmt.Errorf("%s is not valid JSON, so it was left alone: %w", path, err)
		}
	} else if !os.IsNotExist(err) {
		return err
	} else if remove {
		return nil
	}
	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	groups, _ := hooks["Stop"].([]any)
	var kept []any
	for _, g := range groups {
		b, _ := json.Marshal(g)
		if !strings.Contains(string(b), textCheckMarker) {
			kept = append(kept, g)
		}
	}
	if !remove {
		kept = append(kept, map[string]any{"hooks": []any{map[string]any{
			"type": "prompt", "prompt": TextCheckPrompt, "timeout": 30,
		}}})
	}
	if len(kept) == 0 {
		delete(hooks, "Stop")
	} else {
		hooks["Stop"] = kept
	}
	if len(hooks) == 0 {
		delete(settings, "hooks")
	} else {
		settings["hooks"] = hooks
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(settings, "", "  ")
	return os.WriteFile(path, append(b, '\n'), 0o644)
}
