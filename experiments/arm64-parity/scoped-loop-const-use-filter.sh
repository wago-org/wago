#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_SCOPED_LOOP_CONST=1 WAGO_ARM64_EXPERIMENT_SCOPED_LOCAL_BULK_PROOF=1
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache
export WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 ./src/core/encoder/arm64 ./src/core/runtime > experiments/arm64-parity/scoped-loop-const-use-filter-tests.txt 2>&1
go test -tags wago_guardpage -c -o /tmp/parity-scoped-loop-use-filter.test ./experiments/arm64-parity
WAGO_PARITY_CORE_CODE_DIR=/tmp/scoped-loop-use-filter-core /tmp/parity-scoped-loop-use-filter.test -test.run='^TestCachedCoreOracles$' > experiments/arm64-parity/scoped-loop-const-use-filter-core.txt 2>&1
WAGO_PARITY_CODE_DIR=/tmp/scoped-loop-use-filter-app /tmp/parity-scoped-loop-use-filter.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > experiments/arm64-parity/scoped-loop-const-use-filter-app.txt 2>&1
for image in /tmp/scoped-loop-use-filter-core/*.bin; do cmp "$image" "/tmp/scoped-loop-local-bulk-core/${image##*/}"; done
for image in /tmp/scoped-loop-use-filter-app/*.bin; do cmp "$image" "/tmp/scoped-loop-local-bulk-app/${image##*/}"; done
index=0
for variant in old new new old; do
 binary=/tmp/parity-scoped-loop-local-bulk.test
 if [ "$variant" = new ]; then binary=/tmp/parity-scoped-loop-use-filter.test; fi
 "$binary" -test.run=^$ -test.bench='BenchmarkParity/(stats-gini|vision-otsu)/(Compile|Exec)$' -test.benchtime=500ms -test.count=2 > "experiments/arm64-parity/scoped-loop-const-use-filter-app-$index-$variant.txt" 2>&1
 for id in wago/fastfloat/decimal-parse wago/blake3/hash; do
  export WAGO_PARITY_CORE_ID="$id" WAGO_PARITY_VECTOR_LEN=16384
  name=$(printf '%s' "$id" | tr / _)
  "$binary" -test.run=^$ -test.bench='BenchmarkCachedContract/(Compile|Exec)$' -test.benchtime=500ms -test.count=2 > "experiments/arm64-parity/scoped-loop-const-use-filter-$name-$index-$variant.txt" 2>&1
 done
 index=$((index+1))
done
