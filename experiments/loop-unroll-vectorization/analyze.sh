#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
out=${1:-experiments/loop-unroll-vectorization/results/final}
python3 experiments/loop-unroll-vectorization/summarize.py "$out"
benchstat "$out"/A-final-{A,B,H,C,D,E,G,F}.txt > "$out/benchstat-A.txt"
benchstat "$out"/A-final-{H,C,D}.txt > "$out/benchstat-grouping.txt"
benchstat "$out"/A-final-{D,G,F}.txt > "$out/benchstat-chains.txt"
benchstat "$out"/A-address-{A,B,H,C,D,E,G,F}.txt > "$out/benchstat-A-address.txt"
benchstat "$out"/B-{0,1}.txt > "$out/benchstat-B.txt"
benchstat "$out"/CDE-{scalar,count2,count4,guard2,simd2,simd4,vector-f32,vector-i32}.txt > "$out/benchstat-CDE.txt"
benchstat "$out"/f64-{scalar,pair128,wide256,adjacent128,adjacent256}.txt > "$out/benchstat-f64.txt"
benchstat "$out"/rejected-{scalar,count2,count4,vector-f32,vector-i32}.txt > "$out/benchstat-rejected.txt"
benchstat "$out"/corpus-{scalar,count2,vector-f32,vector-i32}.txt > "$out/benchstat-corpus.txt"
benchstat "$out"/default-{baseline,final}.txt > "$out/benchstat-default.txt"
benchstat "$out/default-baseline-extra.txt" "$out/rejected-scalar.txt" "$out/corpus-scalar.txt" > "$out/benchstat-default-extra.txt"
if [[ -e "$out/corpus-execute-scalar.txt" ]]; then
  benchstat "$out"/corpus-execute-{scalar,count2,count4,guard2}.txt > "$out/benchstat-corpus-execute.txt"
fi
if [[ -e "$out/modern-scalar.txt" ]]; then
  benchstat "$out"/modern-{scalar,vector-f32,vector-i32}.txt > "$out/benchstat-modern.txt"
fi
