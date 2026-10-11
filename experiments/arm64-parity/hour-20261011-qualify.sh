#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
unset WAGO_ARM64_EXPERIMENT_REGIONAL_MEMORY_READ WAGO_ARM64_NO_REGIONAL_MEMORY_READ
unset WAGO_ARM64_EXPERIMENT_DOMINATED_INDEXED_KEY WAGO_ARM64_NO_DOMINATED_INDEXED_KEY
unset WAGO_ARM64_EXPERIMENT_BOUNDED_SIGNED_QUOTIENT WAGO_ARM64_NO_BOUNDED_SIGNED_QUOTIENT
unset WAGO_ARM64_EXPERIMENT_WIDE_REUSE_PINS WAGO_ARM64_NO_WIDE_REUSE_PINS
unset WAGO_ARM64_EXPERIMENT_ORDINARY_SHIFTED_IMMEDIATE WAGO_ARM64_NO_ORDINARY_SHIFTED_IMMEDIATE
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 ./src/core/compiler/optimization ./src/core/runtime ./src/core/encoder/arm64 > experiments/arm64-parity/hour-20261011-tests.txt 2>&1
go test -tags wago_guardpage -c -o /tmp/parity-hour-20261011.test ./experiments/arm64-parity
WAGO_PARITY_CORE_CODE_DIR=/tmp/hour-20261011-core /tmp/parity-hour-20261011.test -test.run='^TestCachedCoreOracles$' > experiments/arm64-parity/hour-20261011-core.txt 2>&1
WAGO_PARITY_CODE_DIR=/tmp/hour-20261011-app /tmp/parity-hour-20261011.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > experiments/arm64-parity/hour-20261011-app.txt 2>&1
go test -c -o /tmp/parity-hour-20261011-explicit.test ./experiments/arm64-parity
/tmp/parity-hour-20261011-explicit.test -test.run='^TestCachedCoreOracles$' > experiments/arm64-parity/hour-20261011-core-explicit.txt 2>&1
/tmp/parity-hour-20261011-explicit.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > experiments/arm64-parity/hour-20261011-app-explicit.txt 2>&1
go test ./src/core/compiler/backend/railshot/arm64 ./src/core/compiler/optimization ./src/core/encoder/arm64 > experiments/arm64-parity/hour-20261011-plain-tests.txt 2>&1
go build -tags wago_guardpage -o /tmp/paired-hour-20261011 ./experiments/arm64-parity/paired
export WAGO_ARM64_EXPERIMENT_REGIONAL_MEMORY_READ=0 WAGO_ARM64_EXPERIMENT_DOMINATED_INDEXED_KEY=0 WAGO_ARM64_EXPERIMENT_BOUNDED_SIGNED_QUOTIENT=0 WAGO_ARM64_EXPERIMENT_WIDE_REUSE_PINS=0 WAGO_ARM64_EXPERIMENT_ORDINARY_SHIFTED_IMMEDIATE=0
WAGO_PARITY_CORE_CODE_DIR=/tmp/hour-20261011-off-core /tmp/parity-hour-20261011.test -test.run='^TestCachedCoreOracles$' > experiments/arm64-parity/hour-20261011-off-core.txt 2>&1
WAGO_PARITY_CODE_DIR=/tmp/hour-20261011-off-app /tmp/parity-hour-20261011.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > experiments/arm64-parity/hour-20261011-off-app.txt 2>&1
python3 - <<'IDENTITY'
from pathlib import Path
lines=[]
for kind,count in [('core',46),('app',102)]:
 files=list(Path('/tmp/hour-20261011-off-'+kind).glob('*.bin'))
 assert len(files)==count,(kind,len(files))
 baseline=Path('/tmp/branch-vector-retained-'+kind)
 assert len(list(baseline.glob('*.bin')))==count
 assert all(p.read_bytes()==(baseline/p.name).read_bytes() for p in files),kind
 lines.append(f'All {count} {kind} images with the five new options disabled match c74d retained baseline.')
 on=list(Path('/tmp/hour-20261011-'+kind).glob('*.bin'))
 assert len(on)==count
 changes=[p.name for p in on if p.read_bytes()!=(baseline/p.name).read_bytes()]
 lines.append(f'Combined default changes {len(changes)} {kind} images:')
 lines.extend(changes)
Path('experiments/arm64-parity/hour-20261011-native-proof.txt').write_text('\n'.join(lines)+'\n')
IDENTITY
