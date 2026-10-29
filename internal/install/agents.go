package install

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// copySelf copies the running binary to dst, so hooks keep working after the
// downloaded copy is gone.
func copySelf(dst string) error {
	src, err := os.Executable()
	if err != nil {
		return err
	}
	if s, err := filepath.EvalSymlinks(src); err == nil {
		src = s
	}
	if a, _ := filepath.Abs(dst); a == src {
		return nil // already in place
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

func exeName() string {
	if runtime.GOOS == "windows" {
		return "assume-nothing.exe"
	}
	return "assume-nothing"
}

// commandLine is how a hook calls the binary through the agent's shell:
// PowerShell on Windows (Cursor uses it there), sh elsewhere.
func commandLine(bin string, args ...string) string {
	if runtime.GOOS == "windows" {
		q := "'" + strings.ReplaceAll(bin, "'", "''") + "'"
		return "& " + q + " " + strings.Join(args, " ")
	}
	return strconv.Quote(bin) + " " + strings.Join(args, " ")
}

// CursorEvents are the Cursor hook events assume-nothing answers.
var CursorEvents = []string{"sessionStart", "beforeSubmitPrompt", "preToolUse", "postToolUse", "afterAgentResponse", "stop", "subagentStop"}

const cursorMarker = "assume-nothing"

// Cursor installs assume-nothing for every Cursor project of this user:
// the binary goes to ~/.cursor/assume-nothing/, and the hooks are added to
// ~/.cursor/hooks.json without touching other hooks.
func Cursor(home string, remove bool) (string, error) {
	dir := filepath.Join(home, ".cursor")
	bin := filepath.Join(dir, "assume-nothing", exeName())
	if !remove {
		if err := copySelf(bin); err != nil {
			return "", err
		}
	}
	path := filepath.Join(dir, "hooks.json")
	cfg := map[string]any{}
	if b, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(b, &cfg); err != nil {
			return "", fmt.Errorf("%s is not valid JSON, so it was left alone: %w", path, err)
		}
	} else if !os.IsNotExist(err) {
		return "", err
	} else if remove {
		return path, nil
	}
	if _, ok := cfg["version"]; !ok {
		cfg["version"] = 1
	}
	hooks, _ := cfg["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	for _, ev := range CursorEvents {
		list, _ := hooks[ev].([]any)
		var kept []any
		for _, h := range list {
			if m, ok := h.(map[string]any); ok {
				if c, _ := m["command"].(string); strings.Contains(c, cursorMarker) && strings.Contains(c, "--agent cursor") {
					continue
				}
			}
			kept = append(kept, h)
		}
		if !remove {
			kept = append(kept, map[string]any{"command": commandLine(bin, "hook", "--agent", "cursor", ev), "timeout": 30})
		}
		if len(kept) == 0 {
			delete(hooks, ev)
		} else {
			hooks[ev] = kept
		}
	}
	cfg["hooks"] = hooks
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	b, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
		return "", err
	}
	if remove {
		os.RemoveAll(filepath.Dir(bin))
	}
	return path, nil
}

// OpenCode installs a small plugin into ~/.config/opencode/plugins/ that
// calls the binary before and after each tool call.
func OpenCode(home string, remove bool) (string, error) {
	cfg := os.Getenv("XDG_CONFIG_HOME")
	if cfg == "" {
		cfg = filepath.Join(home, ".config")
	}
	base := filepath.Join(cfg, "opencode")
	bin := filepath.Join(base, "assume-nothing", exeName())
	plugin := filepath.Join(base, "plugins", "assume-nothing.js")
	if remove {
		os.Remove(plugin)
		os.RemoveAll(filepath.Dir(bin))
		return plugin, nil
	}
	if err := copySelf(bin); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(plugin), 0o755); err != nil {
		return "", err
	}
	q, _ := json.Marshal(bin)
	js := strings.Replace(OpenCodePlugin, "__BIN__", string(q), 1)
	return plugin, os.WriteFile(plugin, []byte(js), 0o644)
}

// OpenCodePlugin is the plugin file; __BIN__ becomes the binary's path.
const OpenCodePlugin = `// assume-nothing for OpenCode (experimental), written by "assume-nothing install-opencode".
// https://github.com/kajisho5/assume-nothing
//
// Before each tool call: the first call of a session is stopped with the
// environment probe attached, and a shell command that is not installed is
// stopped once. After each call: it notes what the call looked at.
// If anything goes wrong it stays out of the way.
import { spawnSync } from "node:child_process"

const BIN = __BIN__

function run(event, payload) {
  try {
    const r = spawnSync(BIN, ["hook", "--agent", "codex", event], {
      input: JSON.stringify(payload),
      encoding: "utf8",
      timeout: 15000,
    })
    if (r.status !== 0 || !r.stdout || !r.stdout.trim()) return null
    return JSON.parse(r.stdout)
  } catch {
    return null
  }
}

export const AssumeNothing = async ({ directory }) => ({
  "tool.execute.before": async (input, output) => {
    const out = run("pre-tool-use", {
      session_id: input.sessionID,
      cwd: directory,
      tool_name: input.tool,
      tool_input: (output && output.args) || {},
    })
    const h = out && out.hookSpecificOutput
    if (h && h.permissionDecision === "deny") throw new Error(h.permissionDecisionReason)
  },
  "tool.execute.after": async (input, output) => {
    run("post-tool-use", {
      session_id: input.sessionID,
      cwd: directory,
      tool_name: input.tool,
      tool_input: input.args || {},
      tool_response: (output && output.output) || "",
    })
  },
})
`
