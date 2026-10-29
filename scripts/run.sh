#!/bin/sh
# Runs the assume-nothing binary built for this machine.
# If none is found it says so on stderr and exits 0, so a missing binary
# can never lock the agent out.
here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
root=$(dirname -- "$here")

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

bin="$root/libexec/assume-nothing-$os-$arch"
[ "$os" = windows ] && bin="$bin.exe"

if [ -f "$bin" ] && [ ! -x "$bin" ]; then
  chmod +x "$bin" 2>/dev/null
fi
if [ ! -x "$bin" ]; then
  if command -v assume-nothing >/dev/null 2>&1; then
    bin=assume-nothing
  else
    echo "assume-nothing: no binary for $os-$arch in $root/libexec" >&2
    exit 0
  fi
fi
exec "$bin" "$@"
