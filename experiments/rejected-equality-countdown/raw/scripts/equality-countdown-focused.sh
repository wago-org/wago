#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_EQUALITY_COUNTDOWN=0
python3 - <<'PY'
from pathlib import Path
p=Path('experiments/arm64-parity/equality-countdown-native-changes.txt')
assert p.exists(),'qualification must complete first'
a=[l.split()[1].removesuffix('.bin') for l in p.read_text().splitlines() if l.startswith('app ')]
priority=['audio-adpcm','compiler-register-allocation','geo-point-in-polygon']
s=[n for n in priority if n in a]
s += [n for n in a if n not in s]
assert s,'no affected native images'
s=s[:6]
if 'geo-point-in-polygon' not in s:s.append('geo-point-in-polygon')
Path('experiments/arm64-parity/equality-countdown-timing-selection.txt').write_text(','.join(s))
PY
go build -tags wago_guardpage -o /tmp/paired-equality-countdown ./experiments/arm64-parity/paired
selection=$(cat experiments/arm64-parity/equality-countdown-timing-selection.txt)
for phase in exec compile; do
 /tmp/paired-equality-countdown -option equality-countdown -workloads "$selection" -phase "$phase" -rounds 8 -budget 200ms > "experiments/arm64-parity/equality-countdown-paired-$phase.jsonl" 2>&1
done
python3 experiments/arm64-parity/summarize_paired.py experiments/arm64-parity/equality-countdown-paired-exec.jsonl experiments/arm64-parity/equality-countdown-paired-compile.jsonl > experiments/arm64-parity/equality-countdown-paired-summary.md
