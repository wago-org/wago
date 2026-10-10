#!/bin/sh
set -eu
unset WAGO_ARM64_EXPERIMENT_INTERVAL_CALL_REGIONS WAGO_ARM64_NO_INTERVAL_CALL_REGIONS
export WAGO_SHARED_SCALAR=0
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 ./src/core/compiler/optimization ./src/core/runtime ./src/core/encoder/arm64 > experiments/arm64-parity/interval-call-retain-tests.txt 2>&1
go test -tags wago_guardpage -c -o /tmp/parity-interval-call-retained.test ./experiments/arm64-parity
WAGO_PARITY_CORE_CODE_DIR=/tmp/interval-call-retained-core /tmp/parity-interval-call-retained.test -test.run='^TestCachedCoreOracles$' > experiments/arm64-parity/interval-call-retain-core.txt 2>&1
WAGO_PARITY_CODE_DIR=/tmp/interval-call-retained-app /tmp/parity-interval-call-retained.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > experiments/arm64-parity/interval-call-retain-app.txt 2>&1
WAGO_ARM64_EXPERIMENT_INTERVAL_CALL_REGIONS=0 WAGO_PARITY_CORE_CODE_DIR=/tmp/interval-call-retained-off-core /tmp/parity-interval-call-retained.test -test.run='^TestCachedCoreOracles$' > experiments/arm64-parity/interval-call-retain-off-core.txt 2>&1
WAGO_ARM64_EXPERIMENT_INTERVAL_CALL_REGIONS=0 WAGO_PARITY_CODE_DIR=/tmp/interval-call-retained-off-app /tmp/parity-interval-call-retained.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > experiments/arm64-parity/interval-call-retain-off-app.txt 2>&1
for image in /tmp/interval-call-retained-off-core/*.bin;do cmp "$image" "/tmp/scoped-loop-retained-core/${image##*/}";done
for image in /tmp/interval-call-retained-off-app/*.bin;do cmp "$image" "/tmp/scoped-loop-retained-app/${image##*/}";done
python3 - <<'SCOPE'
from pathlib import Path
lines=[]
for kind in ['core','app']:
 for p in sorted(Path('/tmp/interval-call-retained-'+kind).glob('*.bin')):
  old=Path('/tmp/scoped-loop-retained-'+kind)/p.name
  if p.read_bytes()!=old.read_bytes():lines.append(f'{kind} {p.name} {old.stat().st_size} {p.stat().st_size}')
Path('experiments/arm64-parity/interval-call-retained-native-changes.txt').write_text('\n'.join(lines)+'\n')
SCOPE
go test -c -o /tmp/parity-interval-call-retained-explicit.test ./experiments/arm64-parity
/tmp/parity-interval-call-retained-explicit.test -test.run='^TestCachedCoreOracles$' > experiments/arm64-parity/interval-call-retain-core-explicit.txt 2>&1
/tmp/parity-interval-call-retained-explicit.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > experiments/arm64-parity/interval-call-retain-app-explicit.txt 2>&1
go test ./src/core/compiler/backend/railshot/arm64 > experiments/arm64-parity/interval-call-retain-plain-tests.txt 2>&1
