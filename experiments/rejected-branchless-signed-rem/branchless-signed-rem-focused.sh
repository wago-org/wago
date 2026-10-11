#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_REGIONAL_MEMORY_READ=0 WAGO_ARM64_EXPERIMENT_BRANCHLESS_SIGNED_REM=0
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache
python3 - <<'SELECT'
from pathlib import Path
import json
p=Path('experiments/arm64-parity')
assert (p/'branchless-signed-rem-proof.txt').exists(), 'qualification must complete first'
changed=(p/'branchless-signed-rem-native-changes.txt').read_text().splitlines()
apps=[];core=[]
for line in changed:
 if not line.strip():continue
 kind,name=line.split()
 old=Path('/tmp/leaf-scratch-memory-safe-'+kind)/name
 new=Path('/tmp/branchless-signed-rem-'+kind)/name
 score=abs(old.stat().st_size-new.stat().st_size)
 (apps if kind=='app' else core).append((score,name))
apps=[n.removesuffix('.bin') for _,n in sorted(apps,reverse=True)[:5]]
if apps and 'compiler-register-allocation' not in apps:apps.append('compiler-register-allocation')
manifest=json.loads(Path('/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache/manifest.json').read_text())['workloads']
cores=[];execs=[]
for _,n in sorted(core,reverse=True)[:5]:
 w=next(w for w in manifest if w['id'].replace('/','__')+'.bin'==n)
 name=w['id'].split('/')[-1];cores.append(name)
 if w['oracle']['kind']=='exact_u64' and not w['oracle'].get('memory') and not w.get('host_profile') and not w.get('initialize') and not w.get('input') and w.get('reset') in ('fresh_instance_per_sample','stateless'):execs.append(name)
for kind,names in [('app',apps),('core-compile',cores),('core-exec',execs)]:
 (p/('branchless-signed-rem-selection-'+kind+'.txt')).write_text(','.join(names))
assert apps or cores, 'no changed corpus images; no corpus speedup to measure'
SELECT
for phase in exec compile; do
 selection=$(cat experiments/arm64-parity/branchless-signed-rem-selection-app.txt)
 if [ -n "$selection" ]; then
  /tmp/paired-branchless-signed-rem -option branchless-signed-rem -workloads "$selection" -phase "$phase" -rounds 8 -budget 200ms > "experiments/arm64-parity/branchless-signed-rem-paired-$phase.jsonl" 2>&1
 fi
 selection=$(cat "experiments/arm64-parity/branchless-signed-rem-selection-core-$phase.txt")
 if [ -n "$selection" ]; then
  /tmp/paired-branchless-signed-rem -core-"$phase" -phase "$phase" -corpus "$WAGO_PARITY_CACHE" -option branchless-signed-rem -workloads "$selection" -rounds 8 -budget 200ms > "experiments/arm64-parity/branchless-signed-rem-core-paired-$phase.jsonl" 2>&1
 fi
done
python3 experiments/arm64-parity/summarize_paired.py experiments/arm64-parity/branchless-signed-rem-*paired-*.jsonl > experiments/arm64-parity/branchless-signed-rem-paired-summary.md
