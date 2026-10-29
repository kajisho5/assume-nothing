#!/bin/sh
# Builds every binary and lays out the ready-to-install plugin.
#   scripts/build.sh <version> <out-dir>
# <out-dir>/plugin    -> what goes on the dist branch
# <out-dir>/assets    -> files attached to the GitHub release
set -eu
version=${1:?version}
out=${2:?out dir}
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
case "$out" in
  ""|/|.|..|"$root"|"$HOME") echo "refusing to use '$out' as the output folder" >&2; exit 1 ;;
esac
rm -rf "$out"
mkdir -p "$out/plugin/libexec" "$out/assets"

for target in darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 windows/amd64 windows/arm64; do
  os=${target%/*}
  arch=${target#*/}
  name="assume-nothing-$os-$arch"
  [ "$os" = windows ] && name="$name.exe"
  (cd "$root" && CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath \
    -ldflags "-s -w -X main.version=$version" -o "$out/plugin/libexec/$name" ./cmd/assume-nothing)
  chmod +x "$out/plugin/libexec/$name"
  cp "$out/plugin/libexec/$name" "$out/assets/$name"
  echo "built $name"
done

cp -R "$root/.claude-plugin" "$root/hooks" "$root/scripts" "$root/skills" "$out/plugin/"
rm -f "$out/plugin/.claude-plugin/marketplace.json" "$out/plugin/scripts/build.sh" "$out/plugin/scripts/smoke.sh" "$out/plugin/scripts/setup-repo.sh"
cp "$root/LICENSE" "$root/README.md" "$out/plugin/"

# Codex reads the same hooks (it sets CLAUDE_PLUGIN_ROOT for compatibility);
# it needs its own manifest and a marketplace file at the root.
cp -R "$root/packaging/codex/.codex-plugin" "$root/packaging/codex/.agents" "$out/plugin/"

# Gemini CLI: an extension with its own hook format, published on its own branch.
mkdir -p "$out/gemini"
cp -R "$root/packaging/gemini/." "$out/gemini/"
cp -R "$out/plugin/libexec" "$out/gemini/"
mkdir -p "$out/gemini/scripts"
cp "$root/scripts/run.sh" "$out/gemini/scripts/"
cp "$root/LICENSE" "$root/README.md" "$out/gemini/"

# every manifest carries the version being built
for f in "$out/plugin/.claude-plugin/plugin.json" "$out/plugin/.codex-plugin/plugin.json" "$out/gemini/gemini-extension.json"; do
  sed -i.bak "s/\"version\": *\"[^\"]*\"/\"version\": \"$version\"/" "$f" && rm -f "$f.bak"
done

# a zip of the plugin, for Claude's Customize > Plugins > Upload plugin
python3 - "$out/plugin" "$out/assets/assume-nothing-plugin.zip" <<'PY'
import os, sys, zipfile
src, dst = sys.argv[1], sys.argv[2]
with zipfile.ZipFile(dst, "w", zipfile.ZIP_DEFLATED) as z:
    for r, _, fs in os.walk(src):
        for f in sorted(fs):
            p = os.path.join(r, f)
            z.write(p, os.path.relpath(p, src))
PY

(cd "$out/assets" && if command -v sha256sum >/dev/null 2>&1; then sha256sum assume-nothing-* > checksums.txt; else shasum -a 256 assume-nothing-* > checksums.txt; fi)
echo "plugin laid out in $out/plugin, Gemini extension in $out/gemini"
