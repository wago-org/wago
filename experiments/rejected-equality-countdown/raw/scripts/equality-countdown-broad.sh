#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_EQUALITY_COUNTDOWN=0
python3 - <<'PY'
from pathlib import Path
p=Path('experiments/arm64-parity/equality-countdown-final-proof.txt');assert p.exists(),'final qualification must pass first'
lines=Path('experiments/arm64-parity/equality-countdown-final-native-changes.txt').read_text().splitlines()
a=[l.split()[1].removesuffix('.bin') for l in lines if l.startswith('app ')]
Path('experiments/arm64-parity/equality-countdown-broad-selection.txt').write_text(','.join(a))
PY
go build -tags wago_guardpage -o /tmp/paired-equality-countdown-final ./experiments/arm64-parity/paired
selection=$(cat experiments/arm64-parity/equality-countdown-broad-selection.txt)
for phase in exec compile; do
 /tmp/paired-equality-countdown-final -option equality-countdown -workloads "$selection" -phase "$phase" -rounds 4 -budget 100ms > "experiments/arm64-parity/equality-countdown-broad-$phase.jsonl" 2>&1
done
python3 experiments/arm64-parity/summarize_paired.py experiments/arm64-parity/equality-countdown-broad-exec.jsonl experiments/arm64-parity/equality-countdown-broad-compile.jsonl > experiments/arm64-parity/equality-countdown-broad-summary.md
