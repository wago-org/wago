#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_EARLY_SELECT_GUARD=0
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache
python3 - <<'PY'
from pathlib import Path
p=Path('experiments/arm64-parity/early-select-guard-native-changes.txt')
assert p.exists(), 'screen must finish successfully before timing'
a=[l.split()[1].removesuffix('.bin') for l in p.read_text().splitlines() if l.startswith('app ')]
ordered=[n for n in ['files-glob-match'] if n in a]+[n for n in a if n!='files-glob-match']
assert ordered, 'no affected applications'
selected=ordered[:5]
if 'compiler-register-allocation' not in selected: selected.append('compiler-register-allocation')
Path('experiments/arm64-parity/early-select-guard-timing-selection.txt').write_text(','.join(selected))
PY
go build -tags wago_guardpage -o /tmp/paired-early-select-guard ./experiments/arm64-parity/paired
selection=$(cat experiments/arm64-parity/early-select-guard-timing-selection.txt)
for phase in exec compile; do
 /tmp/paired-early-select-guard -option early-select-guard -workloads "$selection" -phase "$phase" -rounds 8 -budget 200ms > "experiments/arm64-parity/early-select-guard-paired-$phase.jsonl" 2>&1
done

/tmp/paired-early-select-guard -core-compile -corpus "$WAGO_PARITY_CACHE" -option early-select-guard -workloads decimal-parse,compile-match -rounds 8 -budget 200ms > experiments/arm64-parity/early-select-guard-core-paired-compile.jsonl 2>&1
