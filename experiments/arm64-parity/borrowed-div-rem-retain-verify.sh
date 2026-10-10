#!/bin/sh
set -eu
unset WAGO_ARM64_EXPERIMENT_BORROWED_DIV_REM WAGO_ARM64_NO_BORROWED_DIV_REM
export WAGO_SHARED_SCALAR=0
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 ./src/core/compiler/optimization ./src/core/runtime ./src/core/encoder/arm64 > experiments/arm64-parity/borrowed-div-rem-retain-tests.txt 2>&1
go test -tags wago_guardpage -c -o /tmp/parity-borrowed-div-rem-retained.test ./experiments/arm64-parity
WAGO_PARITY_CORE_CODE_DIR=/tmp/borrowed-div-rem-retained-core /tmp/parity-borrowed-div-rem-retained.test -test.run='^TestCachedCoreOracles$' > experiments/arm64-parity/borrowed-div-rem-retain-core.txt 2>&1
WAGO_PARITY_CODE_DIR=/tmp/borrowed-div-rem-retained-app /tmp/parity-borrowed-div-rem-retained.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > experiments/arm64-parity/borrowed-div-rem-retain-app.txt 2>&1
python3 - <<'IDENTITY'
from pathlib import Path
for kind,count in [('core',46),('app',102)]:
 files=list(Path('/tmp/borrowed-div-rem-retained-'+kind).glob('*.bin'))
 assert len(files)==count
 assert all(p.read_bytes()==(Path('/tmp/borrowed-div-rem-concrete-'+kind)/p.name).read_bytes() for p in files),kind
Path('experiments/arm64-parity/borrowed-div-rem-retain-native-proof.txt').write_text('All148default-on signal images match measured/qualified prototype.\n')
IDENTITY
go test -c -o /tmp/parity-borrowed-div-rem-retained-explicit.test ./experiments/arm64-parity
/tmp/parity-borrowed-div-rem-retained-explicit.test -test.run='^TestCachedCoreOracles$' > experiments/arm64-parity/borrowed-div-rem-retain-core-explicit.txt 2>&1
/tmp/parity-borrowed-div-rem-retained-explicit.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > experiments/arm64-parity/borrowed-div-rem-retain-app-explicit.txt 2>&1
go test ./src/core/compiler/backend/railshot/arm64 > experiments/arm64-parity/borrowed-div-rem-retain-plain-tests.txt 2>&1
