#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_EQUALITY_COUNTDOWN=0
go build -tags wago_guardpage -o /tmp/paired-equality-countdown-restricted ./experiments/arm64-parity/paired
for phase in exec compile; do
 /tmp/paired-equality-countdown-restricted -option equality-countdown -workloads bio-lcs,grid-pathfinding,geo-point-in-polygon -phase "$phase" -rounds 12 -budget 300ms -code-dir /tmp/equality-countdown-restricted-confirm-code > "experiments/arm64-parity/equality-countdown-restricted-confirm-$phase.jsonl" 2>&1
done
python3 experiments/arm64-parity/summarize_paired.py experiments/arm64-parity/equality-countdown-restricted-confirm-exec.jsonl experiments/arm64-parity/equality-countdown-restricted-confirm-compile.jsonl > experiments/arm64-parity/equality-countdown-restricted-confirm-summary.md
go build -tags wago_runtime,wago_profile,wago_guardpage -o /tmp/wago-equality-countdown-restricted-profile ./cli/wago
python3 - <<'INNER' > /tmp/equality-countdown-lcs-profile-spec.txt
import json
m=json.load(open('/Users/work/Code/Web/wasm.fyi/corpora/applications/manifest.json'))
w=next(w for w in m if w['id'].endswith('bio-lcs'))
assert len(w['args'])==1 and len(w['oracle']['expected'])==1 and w['reset']=='stateless'
print(w['args'][0],w['oracle']['expected'][0])
INNER
set -- $(cat /tmp/equality-countdown-lcs-profile-spec.txt)
for on in 0 1; do
 WAGO_ARM64_EXPERIMENT_EQUALITY_COUNTDOWN="$on" /tmp/wago-equality-countdown-restricted-profile profile record --module /Users/work/Code/Web/wasm.fyi/corpora/applications/artifacts/bio-lcs.wasm --export benchmark --args "$1" --want "$2" --bounds signals --mode prepared --duration 3s --backend samply --samply /opt/homebrew/bin/samply --rate 1000 --include-code --source-maps --out "experiments/arm64-parity/profile-equality-countdown-lcs-$on" > "experiments/arm64-parity/profile-equality-countdown-lcs-$on.txt" 2>&1
 /tmp/wago-profile-tools/bin/python experiments/arm64-parity/sampled_assembly.py "experiments/arm64-parity/profile-equality-countdown-lcs-$on" > "experiments/arm64-parity/profile-equality-countdown-lcs-$on-assembly.txt"
done
