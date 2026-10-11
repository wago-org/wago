#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_BOUNDED_SIGNED_QUOTIENT=0
rg -q "^PASS$" experiments/arm64-parity/bounded-quotient-explicit-core.txt
rg -q "^PASS$" experiments/arm64-parity/bounded-quotient-explicit-app.txt
for phase in exec compile; do
 /tmp/paired-bounded-quotient -option bounded-signed-quotient -workloads audio-adpcm -phase "$phase" -rounds 12 -budget 400ms > "experiments/arm64-parity/bounded-quotient-confirm-$phase.jsonl" 2>&1
done
python3 experiments/arm64-parity/summarize_paired.py experiments/arm64-parity/bounded-quotient-confirm-exec.jsonl experiments/arm64-parity/bounded-quotient-confirm-compile.jsonl > experiments/arm64-parity/bounded-quotient-confirm-summary.md
go build -tags wago_runtime,wago_profile,wago_guardpage -o /tmp/wago-bounded-quotient-profile ./cli/wago
python3 - <<'INNER'
from pathlib import Path
import json, subprocess, os
base=Path('/Users/work/Code/Web/wasm.fyi/corpora/applications')
w=next(w for w in json.loads((base/'manifest.json').read_text()) if w['id']=='applications/audio-adpcm')
root=Path('experiments/arm64-parity').resolve()
for enabled in [False, True]:
 prefix='profile-bounded-quotient-'+('on' if enabled else 'off')
 env=dict(os.environ,WAGO_ARM64_EXPERIMENT_BOUNDED_SIGNED_QUOTIENT='1' if enabled else '0')
 with (root/(prefix+'.txt')).open('w') as out:
  subprocess.run(['/tmp/wago-bounded-quotient-profile','profile','record','--module',str(base/w['artifact']),'--export',w['export'],'--args',str(w['args'][0]),'--want',w['oracle']['expected'][0],'--bounds','signals','--mode','prepared','--duration','3s','--backend','samply','--samply','/opt/homebrew/bin/samply','--rate','1000','--include-code','--source-maps','--out',str(root/prefix)],check=True,stdout=out,stderr=subprocess.STDOUT,env=env)
 with (root/(prefix+'-assembly.txt')).open('w') as out:
  subprocess.run(['/tmp/wago-profile-tools/bin/python',str(root/'sampled_assembly.py'),str(root/prefix)],check=True,stdout=out)
INNER
