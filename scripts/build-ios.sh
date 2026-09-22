#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TOOLS="${ROOT}/.local/mobile-tools"
VERSION=v0.0.0-20260908204917-8b95e45f8d3e
mkdir -p "${TOOLS}"
GOBIN="${TOOLS}" go install "golang.org/x/mobile/cmd/gomobile@${VERSION}"
GOBIN="${TOOLS}" go install "golang.org/x/mobile/cmd/gobind@${VERSION}"
export PATH="${TOOLS}:${PATH}"
cd "${ROOT}/mobile"
gomobile init
gomobile bind -target=ios,iossimulator -iosversion=17.0 -o "${ROOT}/.local/ComlinkNative.xcframework" .
cd "${ROOT}/ios"
xcodegen generate
