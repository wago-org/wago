#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache
export WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
binary=/tmp/parity-scoped-loop-const.test
if [ ! -x "$binary" ]; then echo 'qualification binary missing'; exit 1; fi
WAGO_ARM64_EXPERIMENT_SCOPED_LOOP_CONST=0 WAGO_PARITY_CORE_CODE_DIR=/tmp/scoped-loop-const-core-off "$binary" -test.run='^TestCachedCoreOracles$' > experiments/arm64-parity/scoped-loop-const-core-off-oracles.txt 2>&1
python3 - <<'SCOPE'
from pathlib import Path
core=sorted(Path('/tmp/scoped-loop-const-core').glob('*.bin'))
app=sorted(Path('/tmp/scoped-loop-const').glob('*.bin'))
if len(core)!=46 or len(app)!=102:raise SystemExit('qualification images incomplete')
for p in Path('/tmp/scoped-loop-const-core-off').glob('*.bin'):
 if p.read_bytes()!=(Path('/tmp/constant-shift-core-before')/p.name).read_bytes():raise SystemExit('disabled prototype changes retained core code: '+p.name)
changed=[p.stem for p in core if p.read_bytes()!=(Path('/tmp/constant-shift-core-before')/p.name).read_bytes()]
changed+=['applications__'+p.stem for p in app if p.read_bytes()!=(Path('/tmp/store-immediate-probe')/p.name).read_bytes()]
Path('experiments/arm64-parity/scoped-loop-const-changed.txt').write_text('\n'.join(changed)+'\n')
SCOPE
for id in wago/fastfloat/decimal-parse wago/pcre2/compile-match wago/xxhash/xxh64-boundaries; do
 export WAGO_PARITY_CORE_ID="$id"
 name=$(printf '%s' "$id" | tr / _)
 index=0
 for variant in off on on off; do
  enabled=0
  if [ "$variant" = on ]; then enabled=1; fi
  WAGO_ARM64_EXPERIMENT_SCOPED_LOOP_CONST="$enabled" "$binary" -test.run=^$ -test.bench='BenchmarkCachedSimple/(Compile|Exec)$' -test.benchtime=300ms -test.count=2 > "experiments/arm64-parity/scoped-loop-const-$name-$index-$variant.txt" 2>&1
  index=$((index+1))
 done
done
