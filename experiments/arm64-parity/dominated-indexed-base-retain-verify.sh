#!/bin/sh
set -eu
unset WAGO_ARM64_EXPERIMENT_DOMINATED_INDEXED_BASE WAGO_ARM64_NO_DOMINATED_INDEXED_BASE
export WAGO_SHARED_SCALAR=0
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 ./src/core/compiler/optimization ./src/core/runtime ./src/core/encoder/arm64 > experiments/arm64-parity/dominated-indexed-base-retain-tests.txt 2>&1
go test -tags wago_guardpage -c -o /tmp/parity-dominated-indexed-base-retained.test ./experiments/arm64-parity
WAGO_PARITY_CORE_CODE_DIR=/tmp/dominated-indexed-base-retained-core /tmp/parity-dominated-indexed-base-retained.test -test.run='^TestCachedCoreOracles$' > experiments/arm64-parity/dominated-indexed-base-retain-core.txt 2>&1
WAGO_PARITY_CODE_DIR=/tmp/dominated-indexed-base-retained-app /tmp/parity-dominated-indexed-base-retained.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > experiments/arm64-parity/dominated-indexed-base-retain-app.txt 2>&1
python3 - <<'IDENTITY'
from pathlib import Path
for kind,count in [('core',46),('app',102)]:
 files=list(Path('/tmp/dominated-indexed-base-retained-'+kind).glob('*.bin'))
 assert len(files)==count
 assert all(p.read_bytes()==(Path('/tmp/dominated-indexed-base-indexed-'+kind)/p.name).read_bytes() for p in files),kind
Path('experiments/arm64-parity/dominated-indexed-base-retain-native-proof.txt').write_text('All148default-on signal images match measured/qualified prototype.\n')
IDENTITY
go test -c -o /tmp/parity-dominated-indexed-base-retained-explicit.test ./experiments/arm64-parity
/tmp/parity-dominated-indexed-base-retained-explicit.test -test.run='^TestCachedCoreOracles$' > experiments/arm64-parity/dominated-indexed-base-retain-core-explicit.txt 2>&1
/tmp/parity-dominated-indexed-base-retained-explicit.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > experiments/arm64-parity/dominated-indexed-base-retain-app-explicit.txt 2>&1
go test ./src/core/compiler/backend/railshot/arm64 > experiments/arm64-parity/dominated-indexed-base-retain-plain-tests.txt 2>&1
