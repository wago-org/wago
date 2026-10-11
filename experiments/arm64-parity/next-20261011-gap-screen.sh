#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0
names=search-aho-corasick,stats-linear-regression,ml-inference,hardware-prime-implicants,serialization-protobuf,audio-fir,grid-pathfinding,ml-kmeans,stats-bootstrap,files-path-normalize,hardware-boolean-anf,ml-convolution
for phase in exec compile; do
 /tmp/paired-next-20261011-base -option ordinary-shifted-immediate -workloads "$names" -phase "$phase" -rounds 6 -budget 125ms > "experiments/arm64-parity/next-20261011-gap-screen-$phase.jsonl" 2>&1
done
python3 experiments/arm64-parity/summarize_paired.py experiments/arm64-parity/next-20261011-gap-screen-*.jsonl > experiments/arm64-parity/next-20261011-gap-screen-summary.md
