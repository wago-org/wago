#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_REGIONAL_MEMORY_READ=0 WAGO_ARM64_EXPERIMENT_BRANCHLESS_SIGNED_REM=1
go test ./src/core/compiler/backend/railshot/arm64 -run '^TestRawSignedRemainderNative$' -v > experiments/arm64-parity/branchless-signed-rem-reuse-native-proof.txt 2>&1
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 ./src/core/compiler/optimization ./src/core/runtime ./src/core/encoder/arm64 > experiments/arm64-parity/branchless-signed-rem-reuse-tests.txt 2>&1
go test -tags wago_guardpage -c -o /tmp/parity-branchless-signed-rem.test ./experiments/arm64-parity
WAGO_PARITY_CORE_CODE_DIR=/tmp/branchless-signed-rem-reuse-core /tmp/parity-branchless-signed-rem.test -test.run='^TestCachedCoreOracles$' > experiments/arm64-parity/branchless-signed-rem-reuse-core.txt 2>&1
WAGO_PARITY_CODE_DIR=/tmp/branchless-signed-rem-reuse-app /tmp/parity-branchless-signed-rem.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > experiments/arm64-parity/branchless-signed-rem-reuse-app.txt 2>&1
go test -c -o /tmp/parity-branchless-signed-rem-reuse-explicit.test ./experiments/arm64-parity
/tmp/parity-branchless-signed-rem-reuse-explicit.test -test.run='^TestCachedCoreOracles$' > experiments/arm64-parity/branchless-signed-rem-reuse-core-explicit.txt 2>&1
/tmp/parity-branchless-signed-rem-reuse-explicit.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > experiments/arm64-parity/branchless-signed-rem-reuse-app-explicit.txt 2>&1
python3 - <<'PY'
from pathlib import Path
changes=[]
for kind,count in [('core',46),('app',102)]:
 files=list(Path('/tmp/branchless-signed-rem-reuse-'+kind).glob('*.bin'));assert len(files)==count
 for p in files:
  old=Path('/tmp/leaf-scratch-memory-safe-'+kind)/p.name
  if p.read_bytes()!=old.read_bytes():changes.append(kind+' '+p.name)
Path('experiments/arm64-parity/branchless-signed-rem-reuse-native-changes.txt').write_text('\n'.join(changes)+'\n')
Path('experiments/arm64-parity/branchless-signed-rem-reuse-proof.txt').write_text('Branchless signed remainder passes diagnostics and 148 exact oracles in both bounds modes; address loans off. Changed signal images: '+str(len(changes))+'\n')
PY

go build -tags wago_guardpage -o /tmp/paired-branchless-signed-rem-reuse ./experiments/arm64-parity/paired
