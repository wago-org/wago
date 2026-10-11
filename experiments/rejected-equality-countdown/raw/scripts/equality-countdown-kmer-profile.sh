#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0
go build -tags wago_runtime,wago_profile,wago_guardpage -o /tmp/wago-equality-countdown-profile ./cli/wago
python3 - <<'PY' > /tmp/equality-countdown-kmer-profile-spec.txt
import json
m=json.load(open('/Users/work/Code/Web/wasm.fyi/corpora/applications/manifest.json'))
w=next(w for w in m if w['id'].endswith('bio-kmer-count'))
assert len(w['args'])==1 and len(w['oracle']['expected'])==1 and w['reset']=='stateless'
print(w['args'][0],w['oracle']['expected'][0])
PY
set -- $(cat /tmp/equality-countdown-kmer-profile-spec.txt)
for on in 0 1; do
 WAGO_ARM64_EXPERIMENT_EQUALITY_COUNTDOWN="$on" /tmp/wago-equality-countdown-profile profile record --module /Users/work/Code/Web/wasm.fyi/corpora/applications/artifacts/bio-kmer-count.wasm --export benchmark --args "$1" --want "$2" --bounds signals --mode prepared --duration 3s --backend samply --samply /opt/homebrew/bin/samply --rate 1000 --include-code --source-maps --out "experiments/arm64-parity/profile-equality-countdown-kmer-$on" > "experiments/arm64-parity/profile-equality-countdown-kmer-$on.txt" 2>&1
 /tmp/wago-profile-tools/bin/python experiments/arm64-parity/sampled_assembly.py "experiments/arm64-parity/profile-equality-countdown-kmer-$on" > "experiments/arm64-parity/profile-equality-countdown-kmer-$on-assembly.txt"
done
