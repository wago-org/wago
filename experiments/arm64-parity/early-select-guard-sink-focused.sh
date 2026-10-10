#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_EARLY_SELECT_GUARD=0
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache
python3 - <<'PY'
from pathlib import Path
p=Path('experiments/arm64-parity/early-select-guard-sink-native-changes.txt')
assert p.exists(), 'screen must finish successfully before timing'
a=[l.split()[1].removesuffix('.bin') for l in p.read_text().splitlines() if l.startswith('app ')]
ordered=[n for n in ['files-glob-match'] if n in a]+[n for n in a if n!='files-glob-match']
assert ordered, 'no affected applications'
selected=ordered[:5]
if 'compiler-register-allocation' not in selected: selected.append('compiler-register-allocation')
Path('experiments/arm64-parity/early-select-guard-sink-timing-selection.txt').write_text(','.join(selected))
PY
go build -tags wago_guardpage -o /tmp/paired-early-select-guard-sink ./experiments/arm64-parity/paired
selection=$(cat experiments/arm64-parity/early-select-guard-sink-timing-selection.txt)
for phase in exec compile; do
 /tmp/paired-early-select-guard-sink -option early-select-guard -workloads "$selection" -phase "$phase" -rounds 12 -budget 300ms > "experiments/arm64-parity/early-select-guard-sink-paired-$phase.jsonl" 2>&1
done

/tmp/paired-early-select-guard-sink -core-compile -phase compile -corpus "$WAGO_PARITY_CACHE" -option early-select-guard -workloads decimal-parse,compile-match -rounds 12 -budget 300ms > experiments/arm64-parity/early-select-guard-sink-core-paired-compile.jsonl 2>&1

python3 experiments/arm64-parity/summarize_paired.py experiments/arm64-parity/early-select-guard-sink-paired-exec.jsonl experiments/arm64-parity/early-select-guard-sink-paired-compile.jsonl experiments/arm64-parity/early-select-guard-sink-core-paired-compile.jsonl > experiments/arm64-parity/early-select-guard-sink-paired-summary.md
