#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_EQUALITY_COUNTDOWN=0
names=$(python3 - <<'INNER'
from pathlib import Path
rows=Path('experiments/arm64-parity/equality-countdown-restricted-native-changes.txt').read_text().splitlines()
print(','.join(row.split()[1][:-4] for row in rows if row.startswith('app ')))
INNER
)
for phase in exec compile; do
 /tmp/paired-equality-countdown-restricted -option equality-countdown -workloads "$names" -phase "$phase" -rounds 4 -budget 100ms > "experiments/arm64-parity/equality-countdown-restricted-screen-$phase.jsonl" 2>&1
done
/tmp/paired-equality-countdown-restricted -corpus /Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache -core-compile -option equality-countdown -workloads decimal-parse,parse-edit-write,inflate -phase compile -rounds 4 -budget 100ms > experiments/arm64-parity/equality-countdown-restricted-screen-core-compile.jsonl 2>&1
python3 experiments/arm64-parity/summarize_paired.py experiments/arm64-parity/equality-countdown-restricted-screen-exec.jsonl experiments/arm64-parity/equality-countdown-restricted-screen-compile.jsonl experiments/arm64-parity/equality-countdown-restricted-screen-core-compile.jsonl > experiments/arm64-parity/equality-countdown-restricted-screen-summary.md
