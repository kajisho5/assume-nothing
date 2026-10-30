# Installs assume-nothing for one agent on Windows, without Git Bash.
# It downloads the latest release for this machine, checks it against the
# release's checksums.txt, and runs the matching install command.
#
#   & ([scriptblock]::Create((irm https://raw.githubusercontent.com/kajisho5/assume-nothing/main/scripts/setup.ps1))) cursor
#   & ([scriptblock]::Create((irm https://raw.githubusercontent.com/kajisho5/assume-nothing/main/scripts/setup.ps1))) opencode
#   & ([scriptblock]::Create((irm https://raw.githubusercontent.com/kajisho5/assume-nothing/main/scripts/setup.ps1))) repo
#
# Add "-Remove" to take it out again. (Claude Code itself runs its hooks
# through Git Bash on Windows; "repo" sets them up, but they only run where
# Git Bash is installed.)
param(
  [Parameter(Position = 0, Mandatory = $true)][ValidateSet("cursor", "opencode", "repo")][string]$Target,
  [switch]$Remove
)
$ErrorActionPreference = "Stop"
$base = "https://github.com/kajisho5/assume-nothing/releases/latest/download"
$arch = if ($env:PROCESSOR_ARCHITECTURE -eq "ARM64") { "arm64" } else { "amd64" }
$name = "assume-nothing-windows-$arch.exe"
$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("assume-nothing-" + [guid]::NewGuid())
New-Item -ItemType Directory -Path $tmp | Out-Null
try {
  $sums = Join-Path $tmp "checksums.txt"
  $exe = Join-Path $tmp $name
  Invoke-WebRequest -UseBasicParsing -Uri "$base/checksums.txt" -OutFile $sums
  Invoke-WebRequest -UseBasicParsing -Uri "$base/$name" -OutFile $exe
  $want = (Get-Content $sums | Where-Object { $_ -match "\s\*?$([regex]::Escape($name))$" } | ForEach-Object { ($_ -split "\s+")[0] }) | Select-Object -First 1
  $got = (Get-FileHash -Algorithm SHA256 -Path $exe).Hash.ToLower()
  if (-not $want -or $got -ne $want.ToLower()) { throw "checksum mismatch for $name; stopping" }
  switch ($Target) {
    "cursor" { if ($Remove) { & $exe install-cursor --remove } else { & $exe install-cursor } }
    "opencode" { if ($Remove) { & $exe install-opencode --remove } else { & $exe install-opencode } }
    "repo" {
      $root = (git rev-parse --show-toplevel 2>$null)
      if (-not $root) { throw "run this inside a git repository" }
      if ($Remove) { & $exe install-repo --remove $root } else { & $exe install-repo --checksums $sums $root }
    }
  }
  if ($LASTEXITCODE -ne 0) { throw "install failed" }
} finally {
  Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}
