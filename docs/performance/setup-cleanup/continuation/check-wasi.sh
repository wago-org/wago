#!/usr/bin/env bash
set -euo pipefail
root=$(git rev-parse --show-toplevel)
cd "$root"
out="$root/docs/performance/setup-cleanup/continuation"
tmp="$root/.tmp/setup-cleanup-next"
export GOMAXPROCS=16 GOGC=100 GOMEMLIMIT=off GODEBUG='' WAGO_BOUNDS=signals
export PATH="/home/jtenner/.local/share/mise/installs/github-web-assembly-wabt/1.0.41/bin:$PATH"
for kind in wasi-focused wasi-phases wasi-host-owned; do
 benchstat "$out/$kind-baseline.txt" "$out/$kind-candidate.txt" > "$out/$kind-benchstat.txt"
done
WAGO_BOUNDS= GOWORK="$tmp/candidate.work" go test -count=1 github.com/wago-org/wasi/... > "$out/test-provider-final.txt" 2>&1
GOWORK="$tmp/candidate.work" go test -tags wago_guardpage -run '^$' -fuzz '^FuzzWASIConstructionIsolation$' -fuzztime=20s ./bench/suite > "$out/test-wasi-fuzz.txt" 2>&1
WAGO_BOUNDS= GOWORK="$tmp/candidate.work" go test -count=1 ./src/wago > "$out/test-wago-final.txt" 2>&1
GOWORK="$tmp/candidate.work" go test -count=1 -tags wago_guardpage ./src/wago > "$out/test-wago-guard-final.txt" 2>&1
WAGO_BOUNDS= GOWORK="$tmp/candidate.work" go test -race -count=1 ./src/wago -run 'Import|Host|Plugin|InstanceClose' > "$out/test-wago-race-final.txt" 2>&1
