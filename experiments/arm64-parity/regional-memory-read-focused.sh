#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_REGIONAL_MEMORY_READ=0
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache
python3 - <<'PY'
from pathlib import Path
p=Path('experiments/arm64-parity/regional-memory-read-native-changes.txt')
assert p.exists(), 'qualification must succeed first'
a=[l.split()[1].removesuffix('.bin') for l in p.read_text().splitlines() if l.startswith('app ')]
priority=['graphics-reed-solomon','video-dct','vision-components','compiler-register-allocation','grid-pathfinding','video-optical-flow']
selected=[n for n in priority if n in a]
selected += [n for n in a if n not in selected]
assert selected, 'no affected application images'
selected=selected[:5]
if 'compiler-register-allocation' not in selected:selected.append('compiler-register-allocation')
Path('experiments/arm64-parity/regional-memory-read-timing-selection.txt').write_text(','.join(selected))
PY
go build -tags wago_guardpage -o /tmp/paired-regional-memory-read ./experiments/arm64-parity/paired
selection=$(cat experiments/arm64-parity/regional-memory-read-timing-selection.txt)
for phase in exec compile; do
 /tmp/paired-regional-memory-read -option regional-memory-read -workloads "$selection" -phase "$phase" -rounds 8 -budget 200ms > "experiments/arm64-parity/regional-memory-read-paired-$phase.jsonl" 2>&1
 /tmp/paired-regional-memory-read -core-"$phase" -phase "$phase" -corpus "$WAGO_PARITY_CACHE" -option regional-memory-read -workloads decimal-parse,modular-arithmetic -rounds 8 -budget 200ms > "experiments/arm64-parity/regional-memory-read-core-paired-$phase.jsonl" 2>&1
done
python3 experiments/arm64-parity/summarize_paired.py experiments/arm64-parity/regional-memory-read-paired-exec.jsonl experiments/arm64-parity/regional-memory-read-paired-compile.jsonl experiments/arm64-parity/regional-memory-read-core-paired-exec.jsonl experiments/arm64-parity/regional-memory-read-core-paired-compile.jsonl > experiments/arm64-parity/regional-memory-read-paired-summary.md
