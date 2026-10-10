#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_REGIONAL_MEMORY_READ=0
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache
for phase in exec compile; do
 names=decimal-parse,complex-roundtrip
 if [ "$phase" = compile ]; then names=decimal-parse,complex-roundtrip,hash; fi
 /tmp/paired-regional-memory-read -core-"$phase" -phase "$phase" -corpus "$WAGO_PARITY_CACHE" -option regional-memory-read -workloads "$names" -rounds 12 -budget 300ms > "experiments/arm64-parity/regional-memory-read-core-confirm-$phase.jsonl" 2>&1
done
python3 experiments/arm64-parity/summarize_paired.py experiments/arm64-parity/regional-memory-read-core-confirm-exec.jsonl experiments/arm64-parity/regional-memory-read-core-confirm-compile.jsonl > experiments/arm64-parity/regional-memory-read-core-confirm-summary.md
export WAGO_PARITY_CORE_ID=wago/blake3/hash
for length in 1024 16384 102400; do
 export WAGO_PARITY_VECTOR_LEN="$length"
for round in 0 1 2 3 4 5; do
 order="0 1"
 if [ $((round % 2)) = 1 ]; then order="1 0"; fi
 for on in $order; do
  WAGO_ARM64_EXPERIMENT_REGIONAL_MEMORY_READ=$on /tmp/parity-regional-memory-read.test -test.run=^$ -test.bench='^BenchmarkCachedContract$' -test.benchtime=200ms > "experiments/arm64-parity/regional-memory-read-blake-$length-$round-$on.txt" 2>&1
 done
done
done
python3 experiments/arm64-parity/regional_memory_vector_summary.py > experiments/arm64-parity/regional-memory-read-vector-summary.md
