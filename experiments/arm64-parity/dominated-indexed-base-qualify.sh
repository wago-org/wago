#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_DOMINATED_INDEXED_BASE=1
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 ./src/core/compiler/optimization ./src/core/encoder/arm64 > experiments/arm64-parity/dominated-indexed-base-tests.txt 2>&1
go test -tags wago_guardpage -c -o /tmp/parity-dominated-indexed-base.test ./experiments/arm64-parity
WAGO_PARITY_CORE_CODE_DIR=/tmp/dominated-indexed-base-core /tmp/parity-dominated-indexed-base.test -test.run='^TestCachedCoreOracles$' > experiments/arm64-parity/dominated-indexed-base-core.txt 2>&1
WAGO_PARITY_CODE_DIR=/tmp/dominated-indexed-base-app /tmp/parity-dominated-indexed-base.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > experiments/arm64-parity/dominated-indexed-base-app.txt 2>&1
python3 - <<'SCOPE'
from pathlib import Path
lines=[]
for kind,base in [('core','arithmetic-zero-off-core'),('app','interval-call-retained-app')]:
 for p in sorted(Path('/tmp/dominated-indexed-base-'+kind).glob('*.bin')):
  old=Path('/tmp/'+base)/p.name
  if p.read_bytes()!=old.read_bytes():lines.append(f'{kind} {p.name} {old.stat().st_size} {p.stat().st_size}')
Path('experiments/arm64-parity/dominated-indexed-base-native-changes.txt').write_text('\n'.join(lines)+'\n')
SCOPE
