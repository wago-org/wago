#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_ORDINARY_SHIFTED_IMMEDIATE=1
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 ./src/core/compiler/optimization ./src/core/runtime ./src/core/encoder/arm64 > experiments/arm64-parity/ordinary-shifted-immediate-tests.txt 2>&1
go test -tags wago_guardpage -c -o /tmp/parity-ordinary-shifted-immediate.test ./experiments/arm64-parity
WAGO_PARITY_CORE_CODE_DIR=/tmp/ordinary-shifted-immediate-core /tmp/parity-ordinary-shifted-immediate.test -test.run='^TestCachedCoreOracles$' > experiments/arm64-parity/ordinary-shifted-immediate-core.txt 2>&1
WAGO_PARITY_CODE_DIR=/tmp/ordinary-shifted-immediate-app /tmp/parity-ordinary-shifted-immediate.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > experiments/arm64-parity/ordinary-shifted-immediate-app.txt 2>&1
python3 - <<'PY'
from pathlib import Path
changes=[]
for kind,count in [('core',46),('app',102)]:
 files=list(Path('/tmp/ordinary-shifted-immediate-'+kind).glob('*.bin'));assert len(files)==count
 for p in files:
  old=Path('/tmp/dominated-indexed-key-'+kind)/p.name
  if p.read_bytes()!=old.read_bytes():changes.append(kind+' '+p.name)
Path('experiments/arm64-parity/ordinary-shifted-immediate-native-changes.txt').write_text('\n'.join(changes)+'\n')
PY
go build -tags wago_guardpage -o /tmp/paired-ordinary-shifted-immediate ./experiments/arm64-parity/paired
