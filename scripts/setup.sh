#!/bin/sh
# Installs assume-nothing for one agent. It downloads the latest release for
# this machine, checks it against the release's checksums.txt, and runs the
# matching install command.
#
#   curl -fsSL https://raw.githubusercontent.com/kajisho5/assume-nothing/main/scripts/setup.sh | sh -s cursor
#   curl -fsSL https://raw.githubusercontent.com/kajisho5/assume-nothing/main/scripts/setup.sh | sh -s opencode
#   curl -fsSL https://raw.githubusercontent.com/kajisho5/assume-nothing/main/scripts/setup.sh | sh -s repo
#
#   cursor    hooks in ~/.cursor/hooks.json, for every Cursor project
#   opencode  a plugin in ~/.config/opencode/plugins/
#   repo      hooks in this git repository (Claude Code, cloud sessions, teams)
#
# Add "--remove" after the target to take it out again.
set -eu
target=${1:-}
shift 2>/dev/null || true
case "$target" in cursor|opencode|repo) ;; *) echo "usage: setup.sh cursor|opencode|repo [--remove]" >&2; exit 2 ;; esac
base="https://github.com/kajisho5/assume-nothing/releases/latest/download"

os=$(uname -s); arch=$(uname -m)
case "$os" in Darwin) os=darwin ;; Linux) os=linux ;; MINGW*|MSYS*|CYGWIN*) os=windows ;; *) echo "unsupported OS: $os" >&2; exit 1 ;; esac
case "$arch" in x86_64|amd64) arch=amd64 ;; arm64|aarch64) arch=arm64 ;; *) echo "unsupported CPU: $arch" >&2; exit 1 ;; esac
name="assume-nothing-$os-$arch"; [ "$os" = windows ] && name="$name.exe"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
fetch() { if command -v curl >/dev/null 2>&1; then curl -fsSL --max-time 120 -o "$2" "$1"; else wget -q -T 120 -O "$2" "$1"; fi; }
fetch "$base/checksums.txt" "$tmp/checksums.txt"
fetch "$base/$name" "$tmp/$name"
want=$(grep " $name\$" "$tmp/checksums.txt" | cut -d' ' -f1)
if command -v sha256sum >/dev/null 2>&1; then got=$(sha256sum "$tmp/$name" | cut -d' ' -f1); else got=$(shasum -a 256 "$tmp/$name" | cut -d' ' -f1); fi
[ -n "$want" ] && [ "$got" = "$want" ] || { echo "checksum mismatch for $name; stopping" >&2; exit 1; }
chmod +x "$tmp/$name"

case "$target" in
  cursor) "$tmp/$name" install-cursor "$@" ;;
  opencode) "$tmp/$name" install-opencode "$@" ;;
  repo)
    root=$(git rev-parse --show-toplevel 2>/dev/null) || { echo "run this inside a git repository" >&2; exit 1; }
    if [ "${1:-}" = "--remove" ]; then "$tmp/$name" install-repo --remove "$root"
    else "$tmp/$name" install-repo --checksums "$tmp/checksums.txt" "$root"; fi ;;
esac
