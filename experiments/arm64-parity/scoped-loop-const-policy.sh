#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_SCOPED_LOOP_CONST=1 WAGO_ARM64_EXPERIMENT_SMALL_MUL_SHIFT=0
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 ./src/core/encoder/arm64 ./src/core/runtime ./src/core/compiler/optimization > experiments/arm64-parity/scoped-loop-const-policy-tests.txt 2>&1
go test -tags wago_guardpage -c -o /tmp/parity-scoped-loop-policy.test ./experiments/arm64-parity
WAGO_PARITY_CORE_CODE_DIR=/tmp/scoped-loop-policy-core /tmp/parity-scoped-loop-policy.test -test.run='^TestCachedCoreOracles$' > experiments/arm64-parity/scoped-loop-const-policy-core.txt 2>&1
WAGO_PARITY_CODE_DIR=/tmp/scoped-loop-policy-app /tmp/parity-scoped-loop-policy.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > experiments/arm64-parity/scoped-loop-const-policy-app.txt 2>&1
for image in /tmp/scoped-loop-policy-core/*.bin; do cmp "$image" "/tmp/scoped-loop-use-filter-core/${image##*/}"; done
for image in /tmp/scoped-loop-policy-app/*.bin; do cmp "$image" "/tmp/scoped-loop-use-filter-app/${image##*/}"; done
WAGO_ARM64_EXPERIMENT_SCOPED_LOOP_CONST=0 WAGO_PARITY_CORE_CODE_DIR=/tmp/scoped-loop-policy-off-core /tmp/parity-scoped-loop-policy.test -test.run='^TestCachedCoreOracles$' > experiments/arm64-parity/scoped-loop-const-policy-off-core.txt 2>&1
WAGO_ARM64_EXPERIMENT_SCOPED_LOOP_CONST=0 WAGO_PARITY_CODE_DIR=/tmp/scoped-loop-policy-off-app /tmp/parity-scoped-loop-policy.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > experiments/arm64-parity/scoped-loop-const-policy-off-app.txt 2>&1
for image in /tmp/scoped-loop-policy-off-core/*.bin; do cmp "$image" "/tmp/constant-shift-core-before/${image##*/}"; done
for image in /tmp/scoped-loop-policy-off-app/*.bin; do cmp "$image" "/tmp/store-immediate-probe/${image##*/}"; done
go test -c -o /tmp/parity-scoped-loop-policy-explicit.test ./experiments/arm64-parity
/tmp/parity-scoped-loop-policy-explicit.test -test.run='^TestCachedCoreOracles$' > experiments/arm64-parity/scoped-loop-const-policy-core-explicit.txt 2>&1
/tmp/parity-scoped-loop-policy-explicit.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > experiments/arm64-parity/scoped-loop-const-policy-app-explicit.txt 2>&1
go build -tags wago_guardpage -o /tmp/paired-scoped-loop-policy ./experiments/arm64-parity/paired
for phase in compile exec; do
 /tmp/paired-scoped-loop-policy -option scoped-loop-int-const -workloads stats-gini,vision-otsu,search-aho-corasick,compiler-register-allocation -phase "$phase" -rounds 8 -budget 250ms > "experiments/arm64-parity/scoped-loop-const-paired-$phase.jsonl" 2>&1
done
