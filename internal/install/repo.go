// Package install sets assume-nothing up inside a repository, so that every
// Claude Code session opened on it gets the hooks: on a laptop, for teammates,
// and in cloud sessions started from a browser or a phone, where plugins from a
// marketplace are not installed.
package install

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Dir is where the launcher lives inside the repository.
const Dir = ".claude/assume-nothing"

// Launcher is the path hooks call, relative to the project root.
const Launcher = Dir + "/run.sh"

const marker = ".claude/assume-nothing/run.sh"

var sumLine = regexp.MustCompile(`^([0-9a-f]{64})\s+\*?(assume-nothing-[a-z0-9]+-[a-z0-9]+(\.exe)?)$`)

// ReadChecksums parses a sha256sum-style file (the release's checksums.txt).
func ReadChecksums(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sums := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if m := sumLine.FindStringSubmatch(strings.TrimSpace(sc.Text())); m != nil {
			sums[m[2]] = m[1]
		}
	}
	if len(sums) == 0 {
		return nil, fmt.Errorf("no checksums found in %s", path)
	}
	return sums, sc.Err()
}

var versionRe = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+([-.][0-9A-Za-z.]+)?$`)

// Repo writes the launcher and adds the hooks to .claude/settings.json in root.
func Repo(root, version string, sums map[string]string) error {
	if !versionRe.MatchString(version) {
		return fmt.Errorf("version %q is not a release version; run this from a released binary", version)
	}
	if len(sums) == 0 {
		return fmt.Errorf("no checksums given")
	}
	if err := os.MkdirAll(filepath.Join(root, Dir), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(root, Launcher), []byte(launcher(version, sums)), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(root, Dir, "README.md"), []byte(readme(version)), 0o644); err != nil {
		return err
	}
	return editSettings(root, true)
}

// Remove takes the hooks out of .claude/settings.json and deletes the launcher folder.
func Remove(root string) error {
	if err := editSettings(root, false); err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(root, Dir))
}

func hookEntry(event string) map[string]any {
	timeout := 30
	if event == "session-start" || event == "user-prompt-submit" {
		timeout = 90 // the first run downloads the binary
	}
	return map[string]any{
		"type":    "command",
		"command": `sh "$CLAUDE_PROJECT_DIR/` + Launcher + `" hook ` + event,
		"timeout": timeout,
		// A hook that cannot run (for example on Windows without Git Bash, where
		// "sh" is missing) must never block the agent.
		"onFailure": "continue",
	}
}

func editSettings(root string, add bool) error {
	path := filepath.Join(root, ".claude", "settings.json")
	settings := map[string]any{}
	if b, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(b, &settings); err != nil {
			return fmt.Errorf("%s is not valid JSON, so it was left alone: %w", path, err)
		}
	} else if !os.IsNotExist(err) {
		return err
	} else if !add {
		return nil
	}
	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	events := []struct{ name, event, matcher string }{
		{"SessionStart", "session-start", ""},
		{"UserPromptSubmit", "user-prompt-submit", ""},
		{"PreToolUse", "pre-tool-use", ""},
		{"PostToolUse", "post-tool-use", ""},
		{"Stop", "stop", ""},
		{"SubagentStop", "subagent-stop", ""},
	}
	for _, e := range events {
		groups, _ := hooks[e.name].([]any)
		var kept []any
		for _, g := range groups {
			if !mentionsLauncher(g) {
				kept = append(kept, g)
			}
		}
		if add {
			group := map[string]any{"hooks": []any{hookEntry(e.event)}}
			if e.matcher != "" {
				group["matcher"] = e.matcher
			}
			kept = append(kept, group)
		}
		if len(kept) == 0 {
			delete(hooks, e.name)
		} else {
			hooks[e.name] = kept
		}
	}
	if len(hooks) == 0 {
		delete(settings, "hooks")
	} else {
		settings["hooks"] = hooks
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

func mentionsLauncher(group any) bool {
	b, _ := json.Marshal(group)
	return strings.Contains(string(b), marker)
}

func launcher(version string, sums map[string]string) string {
	names := make([]string, 0, len(sums))
	for n := range sums {
		names = append(names, n)
	}
	sort.Strings(names)
	var cases strings.Builder
	for _, n := range names {
		fmt.Fprintf(&cases, "  %s) sum=%s ;;\n", n, sums[n])
	}
	return strings.NewReplacer("{{VERSION}}", version, "{{CASES}}", cases.String()).Replace(launcherTemplate)
}

const launcherTemplate = `#!/bin/sh
# assume-nothing launcher, written by "assume-nothing install-repo".
# https://github.com/kajisho5/assume-nothing
#
# On first use it downloads the pinned release for this machine from GitHub,
# checks its SHA-256 against the value below, and keeps it in your cache folder.
# If anything goes wrong it prints a note on stderr and exits 0, so it can never
# lock the agent out.
V="{{VERSION}}"

os=$(uname -s 2>/dev/null)
case "$os" in
  Darwin) os=darwin ;;
  Linux) os=linux ;;
  MINGW*|MSYS*|CYGWIN*|Windows_NT) os=windows ;;
  *) os=unknown ;;
esac
arch=$(uname -m 2>/dev/null)
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) arch=unknown ;;
esac
name="assume-nothing-$os-$arch"
[ "$os" = windows ] && name="$name.exe"

case "$name" in
{{CASES}}  *) echo "assume-nothing: no build for $os-$arch" >&2; exit 0 ;;
esac

dir="${XDG_CACHE_HOME:-${HOME:-/tmp}/.cache}/assume-nothing/$V"
bin="$dir/$name"
if [ ! -x "$bin" ]; then
  mkdir -p "$dir" 2>/dev/null || { echo "assume-nothing: cannot create $dir" >&2; exit 0; }
  url="https://github.com/kajisho5/assume-nothing/releases/download/v$V/$name"
  tmp="$bin.$$.tmp"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL --max-time 60 -o "$tmp" "$url" </dev/null || { rm -f "$tmp"; echo "assume-nothing: download failed: $url" >&2; exit 0; }
  elif command -v wget >/dev/null 2>&1; then
    wget -q -T 60 -O "$tmp" "$url" </dev/null || { rm -f "$tmp"; echo "assume-nothing: download failed: $url" >&2; exit 0; }
  else
    echo "assume-nothing: needs curl or wget to fetch itself" >&2; exit 0
  fi
  if command -v sha256sum >/dev/null 2>&1; then
    got=$(sha256sum "$tmp" | cut -d' ' -f1)
  elif command -v shasum >/dev/null 2>&1; then
    got=$(shasum -a 256 "$tmp" | cut -d' ' -f1)
  else
    rm -f "$tmp"; echo "assume-nothing: cannot verify the download (no sha256sum or shasum)" >&2; exit 0
  fi
  if [ "$got" != "$sum" ]; then
    rm -f "$tmp"; echo "assume-nothing: the download did not match its checksum, so it was not run" >&2; exit 0
  fi
  chmod +x "$tmp" && mv -f "$tmp" "$bin" || { rm -f "$tmp"; exit 0; }
fi
ASSUME_NOTHING_LAUNCHER="$0" exec "$bin" "$@"
`

func readme(version string) string {
	return "# assume-nothing (installed in this repository)\n\n" +
		"These files make every Claude Code session on this repository probe the environment first,\n" +
		"including cloud sessions started from a browser or a phone.\n\n" +
		"- `run.sh` downloads assume-nothing v" + version + " for the current machine on first use,\n" +
		"  checks its SHA-256 against the values written in it, and runs it.\n" +
		"- The hooks are in `.claude/settings.json`.\n\n" +
		"Update: run the setup command again. Remove: `.claude/assume-nothing/run.sh install-repo --remove`,\n" +
		"or delete this folder and the hooks that call `" + Launcher + "`.\n\n" +
		"https://github.com/kajisho5/assume-nothing\n"
}
