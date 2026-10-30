#!/bin/bash
# End-to-end check: build the binary for this machine, then drive the real
# hook entry point (scripts/run.sh) the way the agent would.
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
os=$(go env GOOS); arch=$(go env GOARCH)
name="assume-nothing-$os-$arch"; [ "$os" = windows ] && name="$name.exe"
mkdir -p "$root/libexec"
(cd "$root" && go build -o "libexec/$name" ./cmd/assume-nothing)

work=$(mktemp -d)
export ASSUME_NOTHING_DATA="$work/data"
proj="$work/lumen-demo"            # fictional sample project
mkdir -p "$proj/src"
echo '{"name":"lumen-demo"}' > "$proj/package.json"
cwd_json=$(cd "$proj" && pwd -W 2>/dev/null || pwd)   # Windows path under Git Bash
run() { "$root/scripts/run.sh" hook "$1"; }
fail() { echo "SMOKE FAIL: $*" >&2; exit 1; }

# 1. a tool call before any probe is denied, with the probe attached
out=$(printf '{"session_id":"smoke-1","cwd":"%s","tool_name":"Read","tool_input":{}}' "$cwd_json" | run pre-tool-use) || fail "hook exited non-zero"
grep -q '"permissionDecision":"deny"' <<<"$out" || fail "no deny before probe: $out"
grep -q 'CONFIRMED' <<<"$out" || fail "deny lacks the probe: $out"

# 2. the same call now goes through silently
out=$(printf '{"session_id":"smoke-1","cwd":"%s","tool_name":"Read","tool_input":{}}' "$cwd_json" | run pre-tool-use) || fail "hook exited non-zero"
# (it may carry a note when a slow language was checked again)
grep -q '"permissionDecision":"deny"' <<<"$out" && fail "second call denied: $out"
[ -z "$out" ] || grep -q 'checked again' <<<"$out" || fail "second call not silent: $out"

# 3. session start hands over the snapshot
out=$(printf '{"session_id":"smoke-2","cwd":"%s","source":"startup"}' "$cwd_json" | run session-start) || fail "hook exited non-zero"
grep -q '"additionalContext"' <<<"$out" || fail "no context at session start: $out"
grep -q 'Node (package.json)' <<<"$out" || fail "project not detected: $out"

# 4. a command that does not exist is stopped once, then trusted
cmd='{"session_id":"smoke-2","cwd":"%s","tool_name":"Bash","tool_input":{"command":"zz-fictional-tool --go"}}'
out=$(printf "$cmd" "$cwd_json" | run pre-tool-use) || fail "hook exited non-zero"
grep -q 'not installed' <<<"$out" || fail "missing command not stopped: $out"
out=$(printf "$cmd" "$cwd_json" | run pre-tool-use) || fail "hook exited non-zero"
[ -z "$out" ] || fail "second attempt not allowed: $out"

# 5. garbage input never breaks the agent
out=$(echo 'not json' | run pre-tool-use 2>/dev/null) || fail "bad input must exit 0"
[ -z "$out" ] || fail "bad input should print nothing: $out"

# 6. deep checks run
out=$("$root/scripts/run.sh" deep tool git) || fail "deep tool exited non-zero"
grep -q 'tool.git' <<<"$out" || fail "deep tool failed: $out"

# 7. Gemini CLI shapes through the real entry point
out=$(printf '{"session_id":"smoke-g","cwd":"%s","source":"startup"}' "$cwd_json" | "$root/scripts/run.sh" hook --agent gemini SessionStart) || fail "gemini start exited non-zero"
grep -q '"additionalContext"' <<<"$out" || fail "gemini start lacks context: $out"
out=$(printf '{"session_id":"smoke-g","cwd":"%s","tool_name":"run_shell_command","tool_input":{"command":"zz-fictional-tool --go"}}' "$cwd_json" | "$root/scripts/run.sh" hook --agent gemini BeforeTool) || fail "gemini gate exited non-zero"
grep -q '"decision":"deny"' <<<"$out" || fail "gemini missing command not stopped: $out"

# 8. an answer about something nothing looked up is sent back once
out=$(printf '{"session_id":"smoke-t","cwd":"%s","prompt":"tell me about lumen-kit"}' "$cwd_json" | run user-prompt-submit) || fail "prompt hook exited non-zero"
grep -q 'lumen-kit' <<<"$out" || fail "target not named: $out"
stop='{"session_id":"smoke-t","last_assistant_message":"lumen-kit has 40 components."}'
out=$(echo "$stop" | run stop) || fail "stop hook exited non-zero"
grep -q '"decision":"block"' <<<"$out" || fail "unlooked answer not sent back: $out"
out=$(echo "$stop" | run stop) || fail "stop hook exited non-zero"
[ -z "$out" ] || fail "sent back twice: $out"

# 9. ...and passes once a tool looked it up
printf '{"session_id":"smoke-u","cwd":"%s","prompt":"what is orbit-cli"}' "$cwd_json" | run user-prompt-submit >/dev/null
echo '{"session_id":"smoke-u","tool_name":"WebFetch","tool_input":{"url":"https://example.com/orbit-cli"},"tool_response":"orbit-cli is a fictional build tool. It builds things from a config file."}' | run post-tool-use >/dev/null
out=$(echo '{"session_id":"smoke-u","last_assistant_message":"orbit-cli builds things."}' | run stop) || fail "stop hook exited non-zero"
[ -z "$out" ] || fail "looked-up answer sent back: $out"

echo "smoke OK ($os-$arch)"
rm -rf "$work"
