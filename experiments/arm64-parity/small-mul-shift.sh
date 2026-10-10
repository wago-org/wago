#!/bin/sh
set -eu
test -s experiments/arm64-parity/scoped-loop-const-use-filter-wago_blake3_hash-3-old.txt
rg -q '^PASS$' experiments/arm64-parity/scoped-loop-const-use-filter-wago_blake3_hash-3-old.txt
python3 - <<'PYAPPLY'
from pathlib import Path
import hashlib
root=Path('src/core/compiler/backend/railshot/arm64')
if hashlib.sha256((root/'emit.go').read_bytes()).hexdigest()!='24ba8d8cebf82e1b6ba7f9f6267fc5e5c148cad366dd78f9c0c7d9ae01a33ded':raise SystemExit('emit.go changed before prototype')
(root/'emit.go').write_bytes(Path('experiments/arm64-parity/small-mul-shift-emit.go.txt').read_bytes())
(root/'small_mul_shift.go').write_bytes(Path('experiments/arm64-parity/small-mul-shift.go.txt').read_bytes())
(root/'small_mul_shift_arm64_test.go').write_bytes(Path('experiments/arm64-parity/small-mul-shift-test.go.txt').read_bytes())
PYAPPLY
gofmt -w src/core/compiler/backend/railshot/arm64/emit.go src/core/compiler/backend/railshot/arm64/small_mul_shift.go src/core/compiler/backend/railshot/arm64/small_mul_shift_arm64_test.go
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
