#!/usr/bin/env bash
set -u
root='/tmp/wago-direct-dispatch-osuyzuzw'
export GOMAXPROCS=16 GOGC=100 GOMEMLIMIT=off GODEBUG= WAGO_BOUNDS= GOTOOLCHAIN=local
export PATH="/home/jtenner/.local/share/mise/installs/github-web-assembly-wabt/1.0.41/bin:$PATH"
run() {
 local name=$1
 shift
 "$@" > "$root/checks/$name.txt" 2>&1
 local status=$?
 printf '%s %s\n' "$name" "$status" | tee -a "$root/checks/status.txt"
}
cd "$root/provider"
for version in 1.22.12 1.27.1; do
 export GOROOT="/home/jtenner/.local/share/mise/installs/go/$version" GOWORK=off
 run "provider-$version-race" "$GOROOT/bin/go" test -race -count=1 -overlay "$root/overlay.json" ./...
 run "provider-$version-vet" "$GOROOT/bin/go" vet -overlay "$root/overlay.json" ./...
 export GOWORK="$root/build.work"
 run "provider-$version-current-wago" "$GOROOT/bin/go" test -race -count=1 -overlay "$root/overlay.json" ./internal/core
 cd "$root/wago"
 run "bench-$version-race" "$GOROOT/bin/go" test -race -count=1 -tags wago_guardpage -overlay "$root/overlay.json" ./bench/suite -run 'ImportLifecycle|WASIConstruction|WASIRegistration|WorkerDiagnostic'
 cd "$root/provider"
done
