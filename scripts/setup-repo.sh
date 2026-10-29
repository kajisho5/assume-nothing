#!/bin/sh
# Sets assume-nothing up inside the current git repository, so every Claude Code
# session on it probes first, including cloud sessions started from a browser
# or a phone (they do not install plugins, but they do run a repository's hooks).
#
#   curl -fsSL https://raw.githubusercontent.com/kajisho5/assume-nothing/main/scripts/setup-repo.sh | sh
#
# It downloads the latest release for this machine, checks it against the
# release's checksums.txt, and runs "assume-nothing install-repo", which writes
# .claude/assume-nothing/run.sh and adds three hooks to .claude/settings.json.
set -eu
base="https://github.com/kajisho5/assume-nothing/releases/latest/download"

os=$(uname -s); arch=$(uname -m)
case "$os" in Darwin) os=darwin ;; Linux) os=linux ;; MINGW*|MSYS*|CYGWIN*) os=windows ;; *) echo "unsupported OS: $os" >&2; exit 1 ;; esac
case "$arch" in x86_64|amd64) arch=amd64 ;; arm64|aarch64) arch=arm64 ;; *) echo "unsupported CPU: $arch" >&2; exit 1 ;; esac
name="assume-nothing-$os-$arch"; [ "$os" = windows ] && name="$name.exe"

root=$(git rev-parse --show-toplevel 2>/dev/null) || { echo "run this inside a git repository" >&2; exit 1; }
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

fetch() { if command -v curl >/dev/null 2>&1; then curl -fsSL --max-time 120 -o "$2" "$1"; else wget -q -T 120 -O "$2" "$1"; fi; }
fetch "$base/checksums.txt" "$tmp/checksums.txt"
fetch "$base/$name" "$tmp/$name"

want=$(grep " $name\$" "$tmp/checksums.txt" | cut -d' ' -f1)
if command -v sha256sum >/dev/null 2>&1; then got=$(sha256sum "$tmp/$name" | cut -d' ' -f1); else got=$(shasum -a 256 "$tmp/$name" | cut -d' ' -f1); fi
[ -n "$want" ] && [ "$got" = "$want" ] || { echo "checksum mismatch for $name; stopping" >&2; exit 1; }

chmod +x "$tmp/$name"
"$tmp/$name" install-repo --checksums "$tmp/checksums.txt" "$root"
