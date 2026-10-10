#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0
unset WAGO_ARM64_EXPERIMENT_COMMON_EXIT_COMPARE WAGO_ARM64_NO_COMMON_EXIT_COMPARE
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 ./src/core/compiler/optimization > experiments/arm64-parity/leaf-scoped-const-rollback-tests.txt 2>&1
go test -tags wago_guardpage -c -o /tmp/parity-leaf-scoped-const-rollback.test ./experiments/arm64-parity
WAGO_PARITY_CORE_CODE_DIR=/tmp/leaf-scoped-const-rollback-core /tmp/parity-leaf-scoped-const-rollback.test -test.run='^TestCachedCoreOracles$' > experiments/arm64-parity/leaf-scoped-const-rollback-core.txt 2>&1
WAGO_PARITY_CODE_DIR=/tmp/leaf-scoped-const-rollback-app /tmp/parity-leaf-scoped-const-rollback.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > experiments/arm64-parity/leaf-scoped-const-rollback-app.txt 2>&1
python3 - <<'PY'
from pathlib import Path
for kind,count in [('core',46),('app',102)]:
 files=list(Path('/tmp/leaf-scoped-const-rollback-'+kind).glob('*.bin'))
 assert len(files)==count
 for p in files:assert p.read_bytes()==(Path('/tmp/common-exit-compare-retained-'+kind)/p.name).read_bytes(),(kind,p.name)
Path('experiments/arm64-parity/leaf-scoped-const-rollback-native-proof.txt').write_text('PASS: all148 restored compiler images match retained common-exit baseline.\n')
PY
