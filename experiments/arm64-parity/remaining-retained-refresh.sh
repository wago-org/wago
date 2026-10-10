#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
/tmp/parity-branch-vector-retained.test -test.run=^$ -test.bench='BenchmarkParity/(geo-point-in-polygon|compiler-register-allocation|vision-components|ml-knn|files-glob-match|video-dct|audio-adpcm|vision-dilation|graphics-reed-solomon|language-register-vm)/Exec$' -test.benchtime=100ms -test.count=2 > experiments/arm64-parity/remaining-retained-refresh.txt 2>&1

python3 experiments/arm64-parity/remaining_retained_refresh_report.py > experiments/arm64-parity/remaining-retained-refresh-report.txt
