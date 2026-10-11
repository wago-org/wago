#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_POSITIVE_DIVISOR_TRAPS=1
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
go test -tags wago_guardpage -c -o /tmp/parity-positive-divisor.test ./experiments/arm64-parity
WAGO_PARITY_CORE_CODE_DIR=/tmp/positive-divisor-core /tmp/parity-positive-divisor.test -test.run='^TestCachedCoreOracles$' > experiments/arm64-parity/positive-divisor-core.txt 2>&1
WAGO_PARITY_CODE_DIR=/tmp/positive-divisor-app /tmp/parity-positive-divisor.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > experiments/arm64-parity/positive-divisor-app.txt 2>&1
python3 - <<'PY'
from pathlib import Path
out=[]
for kind,count in [('core',46),('app',102)]:
 files=list(Path('/tmp/positive-divisor-'+kind).glob('*.bin'));assert len(files)==count
 for p in sorted(files):
  old=Path('/tmp/leaf-scratch-memory-safe-'+kind)/p.name
  if p.read_bytes()!=old.read_bytes():out.append(f'{kind} {p.name} {old.stat().st_size} {p.stat().st_size}')
Path('experiments/arm64-parity/positive-divisor-native-changes.txt').write_text('\n'.join(out)+'\n')
PY
