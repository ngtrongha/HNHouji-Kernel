param([Parameter(Mandatory=$true)][string]$NdkHome)
$ErrorActionPreference = 'Stop'
$moduleRootTask = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$ndkRootTask = (Resolve-Path -LiteralPath $NdkHome).Path
if ([IO.File]::ReadAllText((Join-Path $ndkRootTask 'source.properties')) -notmatch '28\.1\.13356709') { throw 'Android NDK r28b required' }
$clangTask = Join-Path $ndkRootTask 'toolchains\llvm\prebuilt\windows-x86_64\bin\clang.exe'
Push-Location $moduleRootTask
try {
  $env:GOOS='android'; $env:GOARCH='arm64'; $env:CGO_ENABLED='1'; $env:GOMAXPROCS='2'
  $env:CC='"' + $clangTask + '" --target=aarch64-linux-android24'
  New-Item -ItemType Directory -Force -Path 'bin' | Out-Null
  go build -p 2 -trimpath -buildmode=pie -ldflags='-s -w' -o bin/houjictl ./cmd/houjictl
  if ($LASTEXITCODE -ne 0) { throw 'Android build failed' }
  go version -m bin/houjictl | Set-Content -LiteralPath 'bin/build-info.txt' -Encoding utf8
} finally { Pop-Location }
