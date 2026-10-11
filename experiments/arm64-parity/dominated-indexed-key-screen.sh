#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_DOMINATED_INDEXED_KEY=1
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 ./src/core/compiler/optimization ./src/core/runtime ./src/core/encoder/arm64 > experiments/arm64-parity/dominated-indexed-key-tests.txt 2>&1
go test -tags wago_guardpage -c -o /tmp/parity-dominated-indexed-key.test ./experiments/arm64-parity
WAGO_PARITY_CORE_CODE_DIR=/tmp/dominated-indexed-key-core /tmp/parity-dominated-indexed-key.test -test.run='^TestCachedCoreOracles$' > experiments/arm64-parity/dominated-indexed-key-core.txt 2>&1
WAGO_PARITY_CODE_DIR=/tmp/dominated-indexed-key-app /tmp/parity-dominated-indexed-key.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > experiments/arm64-parity/dominated-indexed-key-app.txt 2>&1
go test -c -o /tmp/parity-dominated-indexed-key-explicit.test ./experiments/arm64-parity
/tmp/parity-dominated-indexed-key-explicit.test -test.run='^TestCachedCoreOracles$' > experiments/arm64-parity/dominated-indexed-key-core-explicit.txt 2>&1
/tmp/parity-dominated-indexed-key-explicit.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > experiments/arm64-parity/dominated-indexed-key-app-explicit.txt 2>&1
python3 - <<'PY'
from pathlib import Path
changes=[]
for kind,count in [('core',46),('app',102)]:
 files=list(Path('/tmp/dominated-indexed-key-'+kind).glob('*.bin'));assert len(files)==count
 for p in files:
  old=Path('/tmp/regional-memory-retained-'+kind)/p.name
  if p.read_bytes()!=old.read_bytes():changes.append(kind+' '+p.name)
Path('experiments/arm64-parity/dominated-indexed-key-native-changes.txt').write_text('\n'.join(changes)+'\n')
Path('experiments/arm64-parity/dominated-indexed-key-proof.txt').write_text('Keyed address facts pass diagnostics and 148 exact oracles in both bounds modes. Changed signal images: '+str(len(changes))+'\n')
PY

go build -tags wago_guardpage -o /tmp/paired-dominated-indexed-key ./experiments/arm64-parity/paired
