#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_LEAF_SCOPED_CONST=1
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 ./src/core/compiler/optimization > experiments/arm64-parity/leaf-scoped-const-filtered-tests.txt 2>&1
WAGO_CONST_PROBE="$WAGO_PARITY_CORPUS/artifacts/compiler-register-allocation.wasm" go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 -run '^TestScopedConstCorpusLoops$' -v -count=1 > experiments/arm64-parity/leaf-scoped-const-filtered-admission.txt 2>&1
go test -tags wago_guardpage -c -o /tmp/parity-leaf-scoped-const-filtered.test ./experiments/arm64-parity
WAGO_PARITY_CODE_DIR=/tmp/leaf-scoped-const-filtered-app /tmp/parity-leaf-scoped-const-filtered.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > experiments/arm64-parity/leaf-scoped-const-filtered-app.txt 2>&1
python3 - <<'PY'
from pathlib import Path
out=[]
for p in sorted(Path('/tmp/leaf-scoped-const-filtered-app').glob('*.bin')):
 old=Path('/tmp/common-exit-compare-retained-app')/p.name
 if p.read_bytes()!=old.read_bytes():out.append(f'{p.name} {old.stat().st_size} {p.stat().st_size}')
Path('experiments/arm64-parity/leaf-scoped-const-filtered-native-changes.txt').write_text('\n'.join(out)+'\n')
PY
