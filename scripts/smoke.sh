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
echo "$out" | grep -q '"permissionDecision":"deny"' || fail "no deny before probe: $out"
echo "$out" | grep -q 'CONFIRMED' || fail "deny lacks the probe: $out"

# 2. the same call now goes through silently
out=$(printf '{"session_id":"smoke-1","cwd":"%s","tool_name":"Read","tool_input":{}}' "$cwd_json" | run pre-tool-use) || fail "hook exited non-zero"
[ -z "$out" ] || fail "second call not silent: $out"

# 3. session start hands over the snapshot
out=$(printf '{"session_id":"smoke-2","cwd":"%s","source":"startup"}' "$cwd_json" | run session-start) || fail "hook exited non-zero"
echo "$out" | grep -q '"additionalContext"' || fail "no context at session start: $out"
echo "$out" | grep -q 'Node (package.json)' || fail "project not detected: $out"

# 4. a command that does not exist is stopped once, then trusted
cmd='{"session_id":"smoke-2","cwd":"%s","tool_name":"Bash","tool_input":{"command":"zz-fictional-tool --go"}}'
out=$(printf "$cmd" "$cwd_json" | run pre-tool-use) || fail "hook exited non-zero"
echo "$out" | grep -q 'not installed' || fail "missing command not stopped: $out"
out=$(printf "$cmd" "$cwd_json" | run pre-tool-use) || fail "hook exited non-zero"
[ -z "$out" ] || fail "second attempt not allowed: $out"

# 5. garbage input never breaks the agent
out=$(echo 'not json' | run pre-tool-use 2>/dev/null) || fail "bad input must exit 0"
[ -z "$out" ] || fail "bad input should print nothing: $out"

# 6. deep checks run
"$root/scripts/run.sh" deep tool git | grep -q 'tool.git' || fail "deep tool failed"

echo "smoke OK ($os-$arch)"
rm -rf "$work"
