#!/bin/sh
set -eu
unset WAGO_ARM64_EXPERIMENT_REGIONAL_MEMORY_READ WAGO_ARM64_NO_REGIONAL_MEMORY_READ
export WAGO_SHARED_SCALAR=0
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 ./src/core/compiler/optimization ./src/core/runtime ./src/core/encoder/arm64 > experiments/arm64-parity/regional-memory-retain-tests.txt 2>&1
go test -tags wago_guardpage -c -o /tmp/parity-regional-memory-retained.test ./experiments/arm64-parity
WAGO_PARITY_CORE_CODE_DIR=/tmp/regional-memory-retained-core /tmp/parity-regional-memory-retained.test -test.run='^TestCachedCoreOracles$' > experiments/arm64-parity/regional-memory-retain-core.txt 2>&1
WAGO_PARITY_CODE_DIR=/tmp/regional-memory-retained-app /tmp/parity-regional-memory-retained.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > experiments/arm64-parity/regional-memory-retain-app.txt 2>&1
python3 - <<'IDENTITY'
from pathlib import Path
for kind,count in [('core',46),('app',102)]:
 files=list(Path('/tmp/regional-memory-retained-'+kind).glob('*.bin'))
 assert len(files)==count
 assert all(p.read_bytes()==(Path('/tmp/regional-memory-read-qualified-'+kind)/p.name).read_bytes() for p in files),kind
Path('experiments/arm64-parity/regional-memory-retain-native-proof.txt').write_text('All148default-on signal images match measured/qualified prototype.\n')
IDENTITY
go test -c -o /tmp/parity-regional-memory-retained-explicit.test ./experiments/arm64-parity
/tmp/parity-regional-memory-retained-explicit.test -test.run='^TestCachedCoreOracles$' > experiments/arm64-parity/regional-memory-retain-core-explicit.txt 2>&1
/tmp/parity-regional-memory-retained-explicit.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > experiments/arm64-parity/regional-memory-retain-app-explicit.txt 2>&1
go test ./src/core/compiler/backend/railshot/arm64 > experiments/arm64-parity/regional-memory-retain-plain-tests.txt 2>&1
