#!/bin/sh
set -eu
# Previous mitigation must finish qualification and every timing round first.
test -s experiments/arm64-parity/scoped-loop-const-single-decode-fastfloat-5-off.txt
grep -q '^PASS$' experiments/arm64-parity/scoped-loop-const-single-decode-fastfloat-5-off.txt
cp experiments/arm64-parity/scoped-loop-const-local-bulk.go.txt src/core/compiler/backend/railshot/arm64/scoped_loop_const.go
cp experiments/arm64-parity/scoped-loop-const-local-bulk-control.go.txt src/core/compiler/backend/railshot/arm64/control.go
cp experiments/arm64-parity/scoped-loop-const-local-bulk-test.go.txt src/core/compiler/backend/railshot/arm64/scoped_local_bulk_arm64_test.go
gofmt -w src/core/compiler/backend/railshot/arm64/scoped_loop_const.go src/core/compiler/backend/railshot/arm64/control.go src/core/compiler/backend/railshot/arm64/scoped_local_bulk_arm64_test.go
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_SCOPED_LOOP_CONST=1 WAGO_ARM64_EXPERIMENT_SCOPED_LOCAL_BULK_PROOF=1
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache
export WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 ./src/core/encoder/arm64 ./src/core/runtime > experiments/arm64-parity/scoped-loop-const-local-bulk-tests.txt 2>&1
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./experiments/arm64-parity -run '^TestCachedCoreOracles$' > experiments/arm64-parity/scoped-loop-const-local-bulk-core-diagnostics.txt 2>&1
go test -tags wago_guardpage -c -o /tmp/parity-scoped-loop-local-bulk.test ./experiments/arm64-parity
WAGO_PARITY_CORE_CODE_DIR=/tmp/scoped-loop-local-bulk-core /tmp/parity-scoped-loop-local-bulk.test -test.run='^TestCachedCoreOracles$' > experiments/arm64-parity/scoped-loop-const-local-bulk-core.txt 2>&1
WAGO_PARITY_CODE_DIR=/tmp/scoped-loop-local-bulk-app /tmp/parity-scoped-loop-local-bulk.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > experiments/arm64-parity/scoped-loop-const-local-bulk-app.txt 2>&1
go test -c -o /tmp/parity-scoped-loop-local-bulk-explicit.test ./experiments/arm64-parity
/tmp/parity-scoped-loop-local-bulk-explicit.test -test.run='^TestCachedCoreOracles$' > experiments/arm64-parity/scoped-loop-const-local-bulk-core-explicit.txt 2>&1
/tmp/parity-scoped-loop-local-bulk-explicit.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > experiments/arm64-parity/scoped-loop-const-local-bulk-app-explicit.txt 2>&1
python3 - <<'PY'
from pathlib import Path
for kind in ['core','app']:
 a=Path('/tmp/scoped-loop-single-decode-'+kind)
 b=Path('/tmp/scoped-loop-local-bulk-'+kind)
 for image in b.glob('*.bin'):
  if image.read_bytes()!=(a/image.name).read_bytes(): print(kind,image.stem)
PY
for id in wago/fastfloat/decimal-parse wago/pcre2/compile-match wago/xxhash/xxh64-boundaries; do
 export WAGO_PARITY_CORE_ID="$id"
 name=$(printf '%s' "$id" | tr / _)
 index=0
 for variant in old new new old; do
  enabled=0
  if [ "$variant" = new ]; then enabled=1; fi
  WAGO_ARM64_EXPERIMENT_SCOPED_LOCAL_BULK_PROOF="$enabled" /tmp/parity-scoped-loop-local-bulk.test -test.run=^$ -test.bench='BenchmarkCachedSimple/(Compile|Exec)$' -test.benchtime=500ms -test.count=2 > "experiments/arm64-parity/scoped-loop-const-local-bulk-$name-$index-$variant.txt" 2>&1
  index=$((index+1))
 done
done
go build -tags wago_runtime,wago_profile,wago_guardpage -o /tmp/wago-scoped-local-bulk-prof ./cli/wago
artifact=$(python3 -c 'import json,os;from pathlib import Path;p=Path(os.environ["WAGO_PARITY_CACHE"]);w=next(w for w in json.loads((p/"manifest.json").read_text())["workloads"] if w["id"]=="wago/fastfloat/decimal-parse");print(p/w["artifact"])')
/tmp/wago-scoped-local-bulk-prof profile record --module "$artifact" --export fastfloat_run --want 17556528949095414306 --bounds signals --mode prepared --duration 3s --backend samply --samply /opt/homebrew/bin/samply --rate 1000 --include-code --source-maps --out experiments/arm64-parity/profile-scoped-local-bulk-fastfloat > experiments/arm64-parity/profile-scoped-local-bulk-fastfloat.txt 2>&1
/tmp/wago-profile-tools/bin/python experiments/arm64-parity/sampled_assembly.py experiments/arm64-parity/profile-scoped-local-bulk-fastfloat > experiments/arm64-parity/scoped-local-bulk-fastfloat-sampled-assembly.txt
