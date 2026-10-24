#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
: "${ANDROID_NDK_HOME:?Set ANDROID_NDK_HOME to Android NDK r28b (28.1.13356709)}"
grep -q '28.1.13356709' "$ANDROID_NDK_HOME/source.properties"
export GOOS=android GOARCH=arm64 CGO_ENABLED=1 GOMAXPROCS=2
export CC="$ANDROID_NDK_HOME/toolchains/llvm/prebuilt/linux-x86_64/bin/aarch64-linux-android24-clang"
mkdir -p bin
# Android needs Bionic getaddrinfo: a CGO-disabled binary compiles but cannot
# reliably use Android's DNS/Private DNS/VPN resolver.
go build -p 2 -trimpath -buildmode=pie -ldflags='-s -w' -o bin/houjictl ./cmd/houjictl
go version -m bin/houjictl > bin/build-info.txt
