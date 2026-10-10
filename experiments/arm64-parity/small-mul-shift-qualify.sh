#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_SCOPED_LOOP_CONST=0 WAGO_ARM64_EXPERIMENT_SCOPED_LOCAL_BULK_PROOF=0 WAGO_ARM64_EXPERIMENT_SMALL_MUL_SHIFT=1
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 ./src/core/encoder/arm64 ./src/core/runtime > experiments/arm64-parity/small-mul-shift-tests.txt 2>&1
go test -tags wago_guardpage -c -o /tmp/parity-small-mul-shift.test ./experiments/arm64-parity
WAGO_PARITY_CORE_CODE_DIR=/tmp/small-mul-shift-core /tmp/parity-small-mul-shift.test -test.run='^TestCachedCoreOracles$' > experiments/arm64-parity/small-mul-shift-core.txt 2>&1
WAGO_PARITY_CODE_DIR=/tmp/small-mul-shift-app /tmp/parity-small-mul-shift.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > experiments/arm64-parity/small-mul-shift-app.txt 2>&1
python3 - <<'PY'
from pathlib import Path
for kind,baseline in [('core','/tmp/constant-shift-core-before'),('app','/tmp/store-immediate-probe')]:
 for image in Path('/tmp/small-mul-shift-'+kind).glob('*.bin'):
  if image.read_bytes()!=(Path(baseline)/image.name).read_bytes():print(kind,image.stem)
PY
index=0
for variant in off on on off; do
 enabled=0
 if [ "$variant" = on ];then enabled=1;fi
 WAGO_ARM64_EXPERIMENT_SMALL_MUL_SHIFT="$enabled" /tmp/parity-small-mul-shift.test -test.run=^$ -test.bench='BenchmarkParity/(compiler-register-allocation|vision-components|audio-dct|audio-adpcm|text-aho-corasick|stats-gini|vision-otsu)/(Compile|Exec)$' -test.benchtime=400ms -test.count=2 > "experiments/arm64-parity/small-mul-shift-app-$index-$variant.txt" 2>&1
 WAGO_PARITY_CORE_ID=wago/fastfloat/decimal-parse WAGO_ARM64_EXPERIMENT_SMALL_MUL_SHIFT="$enabled" /tmp/parity-small-mul-shift.test -test.run=^$ -test.bench='BenchmarkCachedSimple/(Compile|Exec)$' -test.benchtime=400ms -test.count=2 > "experiments/arm64-parity/small-mul-shift-fastfloat-$index-$variant.txt" 2>&1
 index=$((index+1))
done
