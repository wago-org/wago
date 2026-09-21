#!/usr/bin/env bash
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
export GOMAXPROCS=16
GOWORK="$PWD/.tmp/setup-cleanup-next/baseline.work" go test -c -tags wago_guardpage -o .tmp/setup-cleanup-next/wasi-baseline.test ./bench/suite
GOWORK="$PWD/.tmp/setup-cleanup-next/candidate.work" go test -c -tags wago_guardpage -o .tmp/setup-cleanup-next/wasi-candidate.test ./bench/suite
