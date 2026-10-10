#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 ./src/core/encoder/arm64 > experiments/arm64-parity/constant-floor-tests.txt 2>&1
WAGO_CONST_PROBE="$WAGO_PARITY_CORPUS/artifacts/vision-components.wasm" go test -tags wago_codegenstats,wago_guardpage ./src/core/compiler/backend/railshot/arm64 -run '^TestCorpusConstProbe$' -v > experiments/arm64-parity/components-constant-floor-admission.txt 2>&1
go test -tags wago_guardpage -c -o /tmp/parity-constant-floor.test ./experiments/arm64-parity
WAGO_PARITY_CODE_DIR=/tmp/constant-floor /tmp/parity-constant-floor.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > experiments/arm64-parity/constant-floor-oracles.txt 2>&1
index=0
for variant in before after after before; do
 binary=/tmp/parity-store-immediate-probe-bit.test
 if [ "$variant" = after ]; then binary=/tmp/parity-constant-floor.test; fi
 "$binary" -test.run=^$ -test.bench='BenchmarkParity/(vision-components|compiler-register-allocation|image-resize|search-aho-corasick|video-dct|audio-fir)/(Compile|Exec)$' -test.benchtime=200ms -test.count=2 > "experiments/arm64-parity/constant-floor-$index-$variant.txt" 2>&1
 index=$((index+1))
done
