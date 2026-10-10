#!/bin/sh
set -eu
rm -f experiments/arm64-parity/regional-memory-read-native-changes.txt
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_REGIONAL_MEMORY_READ=1
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 ./src/core/compiler/optimization ./src/core/encoder/arm64 > experiments/arm64-parity/regional-memory-read-tests.txt 2>&1
go test -tags wago_guardpage -c -o /tmp/parity-regional-memory-read.test ./experiments/arm64-parity
WAGO_PARITY_CORE_CODE_DIR=/tmp/regional-memory-read-core /tmp/parity-regional-memory-read.test -test.run='^TestCachedCoreOracles$' > experiments/arm64-parity/regional-memory-read-core.txt 2>&1
WAGO_PARITY_CODE_DIR=/tmp/regional-memory-read-app /tmp/parity-regional-memory-read.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > experiments/arm64-parity/regional-memory-read-app.txt 2>&1
python3 - <<'PY'
from pathlib import Path
out=[]
for kind,count in [('core',46),('app',102)]:
 files=list(Path('/tmp/regional-memory-read-'+kind).glob('*.bin'));assert len(files)==count
 for p in sorted(files):
  old=Path('/tmp/branch-vector-retained-'+kind)/p.name
  if p.read_bytes()!=old.read_bytes():out.append(f'{kind} {p.name} {old.stat().st_size} {p.stat().st_size}')
Path('experiments/arm64-parity/regional-memory-read-native-changes.txt').write_text('\n'.join(out)+'\n')
PY
