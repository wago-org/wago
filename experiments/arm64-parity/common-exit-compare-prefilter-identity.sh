#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_COMMON_EXIT_COMPARE=1
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
go test -tags wago_guardpage -c -o /tmp/parity-common-exit-compare-prefilter.test ./experiments/arm64-parity
WAGO_PARITY_CORE_CODE_DIR=/tmp/common-exit-compare-prefilter-core /tmp/parity-common-exit-compare-prefilter.test -test.run='^TestCachedCoreOracles$' > experiments/arm64-parity/common-exit-compare-prefilter-core.txt 2>&1
WAGO_PARITY_CODE_DIR=/tmp/common-exit-compare-prefilter-app /tmp/parity-common-exit-compare-prefilter.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > experiments/arm64-parity/common-exit-compare-prefilter-app.txt 2>&1
python3 - <<'PY'
from pathlib import Path
count=0
for kind,expected in [('core',46),('app',102)]:
 old=Path('/tmp/common-exit-compare-'+kind)
 new=Path('/tmp/common-exit-compare-prefilter-'+kind)
 original={p.name for p in old.glob('*.bin')}
 updated={p.name for p in new.glob('*.bin')}
 assert len(original)==expected,(kind,len(original))
 assert original==updated,(kind,original^updated)
 for name in original:
  assert (old/name).read_bytes()==(new/name).read_bytes(),(kind,name)
  count+=1
print(f'PASS: all {count} native images identical to fully qualified prefilter baseline')
PY
