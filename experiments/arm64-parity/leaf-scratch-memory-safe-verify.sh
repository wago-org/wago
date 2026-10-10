#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_REGIONAL_MEMORY_READ=0
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 ./src/core/compiler/optimization ./src/core/runtime ./src/core/encoder/arm64 > experiments/arm64-parity/leaf-scratch-memory-safe-tests.txt 2>&1
go test -tags wago_guardpage -c -o /tmp/parity-leaf-scratch-memory-safe.test ./experiments/arm64-parity
WAGO_PARITY_CORE_CODE_DIR=/tmp/leaf-scratch-memory-safe-core /tmp/parity-leaf-scratch-memory-safe.test -test.run='^TestCachedCoreOracles$' > experiments/arm64-parity/leaf-scratch-memory-safe-core.txt 2>&1
WAGO_PARITY_CODE_DIR=/tmp/leaf-scratch-memory-safe-app /tmp/parity-leaf-scratch-memory-safe.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > experiments/arm64-parity/leaf-scratch-memory-safe-app.txt 2>&1
go test -c -o /tmp/parity-leaf-scratch-memory-safe-explicit.test ./experiments/arm64-parity
/tmp/parity-leaf-scratch-memory-safe-explicit.test -test.run='^TestCachedCoreOracles$' > experiments/arm64-parity/leaf-scratch-memory-safe-core-explicit.txt 2>&1
/tmp/parity-leaf-scratch-memory-safe-explicit.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > experiments/arm64-parity/leaf-scratch-memory-safe-app-explicit.txt 2>&1
python3 - <<'PY'
from pathlib import Path
changes=[]
for kind,count in [('core',46),('app',102)]:
 files=list(Path('/tmp/leaf-scratch-memory-safe-'+kind).glob('*.bin'));assert len(files)==count
 for p in files:
  old=Path('/tmp/branch-vector-retained-'+kind)/p.name
  if p.read_bytes()!=old.read_bytes():changes.append(kind+' '+p.name)
Path('experiments/arm64-parity/leaf-scratch-memory-safe-native-changes.txt').write_text('\n'.join(changes)+'\n')
Path('experiments/arm64-parity/leaf-scratch-memory-safe-proof.txt').write_text('Correctness fix passes diagnostics and148exact oracles in both bounds modes; separate loan option off. Changed signal images: '+str(len(changes))+'\n')
PY
