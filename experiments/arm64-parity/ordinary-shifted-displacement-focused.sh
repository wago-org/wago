#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_ORDINARY_SHIFTED_IMMEDIATE=0
names=$(python3 - <<'PY'
from pathlib import Path
s=Path('experiments/arm64-parity/ordinary-shifted-displacement-native-changes.txt').read_text().splitlines()
print(','.join(x.split()[1][:-4] for x in s if x.startswith('app ')))
PY
)
for phase in exec compile; do
 /tmp/paired-ordinary-shifted-displacement -option ordinary-shifted-immediate -workloads "$names" -phase "$phase" -rounds 4 -budget 100ms > "experiments/arm64-parity/ordinary-shifted-displacement-focused-$phase.jsonl" 2>&1
done
python3 experiments/arm64-parity/summarize_paired.py experiments/arm64-parity/ordinary-shifted-displacement-focused-*.jsonl > experiments/arm64-parity/ordinary-shifted-displacement-focused-summary.md
