#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_SCOPED_LOOP_CONST=1
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache
export WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
python3 - <<'PYAPPLY'
from pathlib import Path
import hashlib
checks={'control.go': '41c47ffb4befd7aa3e432b25a7ac29c72b144dc2e86eb5e469d172a34a06e288', 'scoped_loop_const.go': '1b20811bf1ad18ce965f33c8eb97c0089cb3856e0bb6de3cd8b9c95feef8028c'}
root=Path('src/core/compiler/backend/railshot/arm64')
for name,want in checks.items():
 if hashlib.sha256((root/name).read_bytes()).hexdigest()!=want: raise SystemExit('source changed before mitigation: '+name)
(root/'control.go').write_bytes(Path('experiments/arm64-parity/scoped-loop-const-single-decode-control.go.txt').read_bytes())
(root/'scoped_loop_const.go').write_bytes(Path('experiments/arm64-parity/scoped-loop-const-single-decode.go.txt').read_bytes())
PYAPPLY
gofmt -w src/core/compiler/backend/railshot/arm64/control.go src/core/compiler/backend/railshot/arm64/scoped_loop_const.go
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 ./src/core/encoder/arm64 ./src/core/runtime > experiments/arm64-parity/scoped-loop-const-single-decode-tests.txt 2>&1
go test -tags wago_guardpage -c -o /tmp/parity-scoped-loop-single-decode.test ./experiments/arm64-parity
WAGO_PARITY_CORE_CODE_DIR=/tmp/scoped-loop-single-decode-core /tmp/parity-scoped-loop-single-decode.test -test.run='^TestCachedCoreOracles$' > experiments/arm64-parity/scoped-loop-const-single-decode-core.txt 2>&1
WAGO_PARITY_CODE_DIR=/tmp/scoped-loop-single-decode-app /tmp/parity-scoped-loop-single-decode.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > experiments/arm64-parity/scoped-loop-const-single-decode-app.txt 2>&1
for image in /tmp/scoped-loop-single-decode-core/*.bin; do cmp "$image" "/tmp/scoped-loop-const-core/${image##*/}"; done
for image in /tmp/scoped-loop-single-decode-app/*.bin; do cmp "$image" "/tmp/scoped-loop-const/${image##*/}"; done
index=0
for variant in off old new new old off; do
 binary=/tmp/parity-scoped-loop-single-decode.test
 enabled=1
 if [ "$variant" = off ]; then enabled=0; fi
 if [ "$variant" = old ]; then binary=/tmp/parity-scoped-loop-const.test; fi
 WAGO_ARM64_EXPERIMENT_SCOPED_LOOP_CONST="$enabled" "$binary" -test.run=^$ -test.bench='BenchmarkParity/(stats-gini|vision-otsu)/(Compile|Exec)$' -test.benchtime=500ms -test.count=2 > "experiments/arm64-parity/scoped-loop-const-single-decode-app-$index-$variant.txt" 2>&1
 WAGO_PARITY_CORE_ID=wago/fastfloat/decimal-parse WAGO_ARM64_EXPERIMENT_SCOPED_LOOP_CONST="$enabled" "$binary" -test.run=^$ -test.bench='BenchmarkCachedSimple/(Compile|Exec)$' -test.benchtime=500ms -test.count=2 > "experiments/arm64-parity/scoped-loop-const-single-decode-fastfloat-$index-$variant.txt" 2>&1
 index=$((index+1))
done
