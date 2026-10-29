#!/bin/sh
# Builds every binary and lays out the ready-to-install plugin.
#   scripts/build.sh <version> <out-dir>
# <out-dir>/plugin    -> what goes on the dist branch
# <out-dir>/assets    -> files attached to the GitHub release
set -eu
version=${1:?version}
out=${2:?out dir}
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
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
rm -f "$out/plugin/.claude-plugin/marketplace.json" "$out/plugin/scripts/build.sh" "$out/plugin/scripts/smoke.sh"
cp "$root/LICENSE" "$root/README.md" "$out/plugin/"

(cd "$out/assets" && if command -v sha256sum >/dev/null 2>&1; then sha256sum assume-nothing-* > checksums.txt; else shasum -a 256 assume-nothing-* > checksums.txt; fi)
echo "plugin laid out in $out/plugin"
