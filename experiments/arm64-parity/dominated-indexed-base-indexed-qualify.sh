#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_DOMINATED_INDEXED_BASE=1
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 ./src/core/compiler/optimization ./src/core/encoder/arm64 > experiments/arm64-parity/dominated-indexed-base-indexed-tests.txt 2>&1
go test -tags wago_guardpage -c -o /tmp/parity-dominated-indexed-base-indexed.test ./experiments/arm64-parity
WAGO_PARITY_CORE_CODE_DIR=/tmp/dominated-indexed-base-indexed-core /tmp/parity-dominated-indexed-base-indexed.test -test.run='^TestCachedCoreOracles$' > experiments/arm64-parity/dominated-indexed-base-indexed-core.txt 2>&1
WAGO_PARITY_CODE_DIR=/tmp/dominated-indexed-base-indexed-app /tmp/parity-dominated-indexed-base-indexed.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > experiments/arm64-parity/dominated-indexed-base-indexed-app.txt 2>&1
python3 - <<'SCOPE'
from pathlib import Path
lines=[]
for kind,base in [('core','arithmetic-zero-off-core'),('app','interval-call-retained-app')]:
 for p in sorted(Path('/tmp/dominated-indexed-base-indexed-'+kind).glob('*.bin')):
  old=Path('/tmp/'+base)/p.name
  if p.read_bytes()!=old.read_bytes():lines.append(f'{kind} {p.name} {old.stat().st_size} {p.stat().st_size}')
Path('experiments/arm64-parity/dominated-indexed-base-indexed-native-changes.txt').write_text('\n'.join(lines)+'\n')
SCOPE

python3 - <<'IDENTITY'
from pathlib import Path
for kind,count in [('core',46),('app',102)]:
 files=list(Path('/tmp/dominated-indexed-base-indexed-'+kind).glob('*.bin'))
 assert len(files)==count
 assert all(p.read_bytes()==(Path('/tmp/dominated-indexed-base-bce-'+kind)/p.name).read_bytes() for p in files),kind
Path('experiments/arm64-parity/dominated-indexed-base-indexed-native-proof.txt').write_text('All148signalnativeimagesidentical to the measured linear version.\n')
IDENTITY
