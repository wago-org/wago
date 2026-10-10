#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_ARITHMETIC_ZERO=1
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 ./src/core/compiler/optimization ./src/core/encoder/arm64 > experiments/arm64-parity/arithmetic-zero-enabled-tests.txt 2>&1
go test -tags wago_guardpage -c -o /tmp/parity-arithmetic-zero.test ./experiments/arm64-parity
WAGO_PARITY_CORE_CODE_DIR=/tmp/arithmetic-zero-core /tmp/parity-arithmetic-zero.test -test.run='^TestCachedCoreOracles$' > experiments/arm64-parity/arithmetic-zero-core.txt 2>&1
WAGO_PARITY_CODE_DIR=/tmp/arithmetic-zero-app /tmp/parity-arithmetic-zero.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > experiments/arm64-parity/arithmetic-zero-app.txt 2>&1
python3 - <<'SCOPE'
from pathlib import Path
lines=[]
for kind in ['core','app']:
 for p in sorted(Path('/tmp/arithmetic-zero-'+kind).glob('*.bin')):
  old=Path('/tmp/interval-call-retained-'+kind)/p.name
  if p.read_bytes()!=old.read_bytes():lines.append(f'{kind} {p.name} {old.stat().st_size} {p.stat().st_size}')
Path('experiments/arm64-parity/arithmetic-zero-native-changes.txt').write_text('\n'.join(lines)+'\n')
SCOPE
