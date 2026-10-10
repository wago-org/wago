#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_INTERVAL_CALL_REGIONS=1
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 ./src/core/compiler/optimization > experiments/arm64-parity/interval-call-regions-enabled-tests.txt 2>&1
go test -tags wago_guardpage -c -o /tmp/parity-interval-call-regions.test ./experiments/arm64-parity
WAGO_PARITY_CORE_CODE_DIR=/tmp/interval-call-regions-core /tmp/parity-interval-call-regions.test -test.run='^TestCachedCoreOracles$' > experiments/arm64-parity/interval-call-regions-core.txt 2>&1
WAGO_PARITY_CODE_DIR=/tmp/interval-call-regions-app /tmp/parity-interval-call-regions.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > experiments/arm64-parity/interval-call-regions-app.txt 2>&1
python3 - <<'CHECK'
from pathlib import Path
lines=[]
for kind in ['core','app']:
 for p in sorted(Path('/tmp/interval-call-regions-'+kind).glob('*.bin')):
  before=Path('/tmp/scoped-loop-retained-'+kind)/p.name
  if p.read_bytes()!=before.read_bytes():lines.append(f'{kind} {p.name} {before.stat().st_size} {p.stat().st_size}')
Path('experiments/arm64-parity/interval-call-regions-native-changes.txt').write_text('\n'.join(lines)+'\n')
CHECK
go test -c -o /tmp/parity-interval-call-regions-explicit.test ./experiments/arm64-parity
/tmp/parity-interval-call-regions-explicit.test -test.run='^TestCachedCoreOracles$' > experiments/arm64-parity/interval-call-regions-core-explicit.txt 2>&1
