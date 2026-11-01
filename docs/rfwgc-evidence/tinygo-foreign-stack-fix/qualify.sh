#!/usr/bin/env bash
# Correctness qualification only. No benchmark or timing commands.
set -euo pipefail
repo=$(git rev-parse --show-toplevel)
evidence="${WAGO_EVIDENCE_DIR:-$repo/docs/rfwgc-evidence/tinygo-foreign-stack-fix}"
mkdir -p "$evidence"
head=$(git rev-parse "${WAGO_QUALIFICATION_HEAD:-HEAD}")
source_tree=$(mktemp -d /tmp/wago-foreign-stack-source.XXXXXX)
cache=$(mktemp -d /tmp/wago-foreign-stack-cache.XXXXXX)
git archive "$head" | tar -x -C "$source_tree"
# Export the pinned Release 3 dependency without touching dirty submodules.
git -C /home/dev/Projects/wago/tests/conformance/spec-v3 archive 9d36019973201a19f9c9ebb0f10828b2fe2374aa | tar -x -C "$source_tree/tests/conformance/spec-v3"
export GOROOT=/home/dev/.local/share/mise/installs/go/1.27.1
export PATH=/home/dev/.local/share/wago-tinygo-investigation/tinygo/bin:$GOROOT/bin:/home/dev/Projects/wago/.tools/wabt-1.0.41-linux-x64/bin:/home/dev/.local/share/mise/installs/github-bytecodealliance-wasm-tools/1.251.0:$PATH
export GOTOOLCHAIN=local GOMAXPROCS=2 GOFLAGS=-p=2
export XDG_CACHE_HOME="$cache" GOCACHE="$cache/go-build"
export GOWORK=off
export WAGO_SPEC_INTERPRETER="$repo/.tools/stage3-full-suite/current/.tools/spec-interpreter-9d36019973201a19f9c9ebb0f10828b2fe2374aa/wasm"
export WAGO_SPEC_INTERPRETER_REVISION=9d36019973201a19f9c9ebb0f10828b2fe2374aa
export WAGO_WAST2JSON=/home/dev/Projects/wago/.tools/wabt-1.0.41-linux-x64/bin/wast2json WAGO_WABT_VERSION=1.0.41
unset WAGO_UPDATE_STAGED_SPEC WAGO_SPEC_FILES WAGO_SPECTEST_DIR WAGO_SPEC_VERSION
{
 printf 'head=%s\nsource=%s\nfresh_cache=%s\n' "$head" "$source_tree" "$cache"
 go version
 tinygo version
 wat2wasm --version
 wasm-tools --version
 sha256sum "$(command -v tinygo)" "$source_tree/scripts/tinygo-042/wago_foreign_stack.go.in"
} > "$evidence/qualification-provenance.txt"
cd "$source_tree"
scripts/patch-tinygo-042.sh > "$evidence/qualification-bootstrap.log" 2>&1
python3 tests/scripts/patch-tinygo-foreign-stack.py >> "$evidence/qualification-bootstrap.log" 2>&1
: > "$evidence/qualification-results.txt"
run() {
 local label=$1
 shift
 local command_text
 printf -v command_text '%q ' "$@"
 printf '%s\n' "${command_text% }" > "$evidence/$label.command"
 local rc=0
 timeout 30m "$@" > "$evidence/$label.log" 2>&1 || rc=$?
 printf '%s exit=%s\n' "$label" "$rc" >> "$evidence/qualification-results.txt"
}
for mode in ordinary wagodebug wago_regalloccheck; do
 tags=wago_runtime
 if [[ $mode != ordinary ]]; then tags+=",$mode"; fi
 run "host-$mode" go test -count=1 -timeout=25m -tags="$tags" ./src/core/runtime ./src/wago
 if [[ $mode == ordinary ]]; then
  pattern='^(TestBuildTinyGoLinksMachineCodeWithoutCompiler|TestBuildTinyGoStripsByDefault|TestTinyGoSupportsCooperativeInterruption|TestTinyGoForeignStackGCInterruption)$'
 else
  # The three explicit guest modes already run above; host mode is separate.
  pattern='^TestTinyGoSupportsCooperativeInterruption$'
 fi
 run "standalone-$mode" go test -count=1 -timeout=25m -tags="$tags" -run "$pattern" ./cli/manager/internal/standalone
 run "guest-threads-$mode" tinygo test -scheduler=threads -gc=conservative -tags="$tags" ./src/core/runtime ./src/wago
 done
# CI's Linux amd64 runtime/checker recipe uses scheduler=tasks and regalloccheck.
run ci-checkers tinygo test -tags=wago_regalloccheck -scheduler=tasks ./internal/regalloccheck ./src/core/encoder/amd64 ./src/core/encoder/arm64
run ci-runtime tinygo test -tags=wago_regalloccheck -scheduler=tasks ./src/core/runtime ./src/wago
run ci-cli tinygo build -scheduler=tasks -tags=wago_runtime,wago_lean,wago_minimal -o "$source_tree/wago-tinygo" ./cli/wago
run ci-cli-smoke "$source_tree/wago-tinygo" run --invoke fib tests/fixtures/wasm/fib.wasm 20
run ci-release tinygo build -scheduler=tasks -no-debug -opt=z -gc=conservative -tags=wago_runtime,wago_lean,wago_minimal -o "$source_tree/wago-tinygo-release" ./cli/wago
for iteration in {1..10}; do
 run "ci-release-version-$iteration" "$source_tree/wago-tinygo-release" --version
 run "ci-release-fib-$iteration" "$source_tree/wago-tinygo-release" run --invoke fib tests/fixtures/wasm/fib.wasm 20
 done
python3 - "$evidence" <<'PYVALUES'
from pathlib import Path
import sys
root=Path(sys.argv[1])
for name in ['ci-cli-smoke'] + [f'ci-release-fib-{i}' for i in range(1,11)]:
 assert (root/(name+'.log')).read_text().strip() == '6765', name
(root/'ci-fib-values.txt').write_text('PASS: all eleven fib outputs equal 6765\n')
PYVALUES
if grep -v 'exit=0$'  "$evidence/qualification-results.txt"; then exit 1; fi
