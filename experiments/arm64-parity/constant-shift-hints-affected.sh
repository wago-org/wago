#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
python3 - <<'SCOPE'
from pathlib import Path
old=Path('/tmp/store-immediate-probe')
new=Path('/tmp/constant-shift-hints')
files=sorted(new.glob('*.bin'))
if len(files)!=102: raise SystemExit('qualification images incomplete')
changed=[p.stem for p in files if p.read_bytes()!=(old/p.name).read_bytes()]
Path('experiments/arm64-parity/constant-shift-hints-changed.txt').write_text('\n'.join(changed)+'\n')
Path('experiments/arm64-parity/constant-shift-hints-subset.txt').write_text('|'.join(changed[:8]))
SCOPE
subset=$(cat experiments/arm64-parity/constant-shift-hints-subset.txt)
if [ -z "$subset" ]; then exit 0; fi
index=0
for variant in before after after before; do
 binary=/tmp/parity-store-immediate-probe-bit.test
 if [ "$variant" = after ]; then binary=/tmp/parity-constant-shift-hints.test; fi
 "$binary" -test.run=^$ -test.bench="BenchmarkParity/($subset)/(Compile|Exec)$" -test.benchtime=300ms -test.count=2 > "experiments/arm64-parity/constant-shift-hints-$index-$variant.txt" 2>&1
 index=$((index+1))
done
