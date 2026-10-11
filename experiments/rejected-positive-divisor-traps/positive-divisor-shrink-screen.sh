#!/bin/sh
set -eu
rg -q "^PASS$" experiments/arm64-parity/positive-divisor-shrink-proof-tests.txt
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_POSITIVE_DIVISOR_TRAPS=1 WAGO_ARM64_POSITIVE_DIVISOR_SHRINK=1 WAGO_FINALIZE_VALIDATE=1
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
go test -tags wago_guardpage -c -o /tmp/parity-positive-divisor-shrink.test ./experiments/arm64-parity
WAGO_PARITY_CORE_CODE_DIR=/tmp/positive-divisor-shrink-core /tmp/parity-positive-divisor-shrink.test -test.run='^TestCachedCoreOracles$' > experiments/arm64-parity/positive-divisor-shrink-core.txt 2>&1
WAGO_PARITY_CODE_DIR=/tmp/positive-divisor-shrink-app /tmp/parity-positive-divisor-shrink.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > experiments/arm64-parity/positive-divisor-shrink-app.txt 2>&1
python3 - <<'PY'
from pathlib import Path
out=[]
for kind,count in [('core',46),('app',102)]:
 files=list(Path('/tmp/positive-divisor-shrink-'+kind).glob('*.bin'));assert len(files)==count
 for p in sorted(files):
  old=Path('/tmp/leaf-scratch-memory-safe-'+kind)/p.name
  if p.read_bytes()!=old.read_bytes():out.append(f'{kind} {p.name} {old.stat().st_size} {p.stat().st_size}')
Path('experiments/arm64-parity/positive-divisor-shrink-native-changes.txt').write_text('\n'.join(out)+'\n')
PY
