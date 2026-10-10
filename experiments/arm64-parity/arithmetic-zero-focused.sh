#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_ARITHMETIC_ZERO=0
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
rg -q '^PASS$' experiments/arm64-parity/arithmetic-zero-core.txt
test -s experiments/arm64-parity/arithmetic-zero-native-changes.txt
python3 - <<'SELECT'
from pathlib import Path
import re,json
changes=Path('experiments/arm64-parity/arithmetic-zero-native-changes.txt').read_text().splitlines()
apps={line.split()[1].removesuffix('.bin') for line in changes if line.startswith('app ')}
ranking=re.findall(r'\| \d+ \| `applications/([^`]+)`',Path('steady-exec-wago-vs-w2c2.md').read_text())
selected=[name for name in ranking if name in apps][:8]
Path('experiments/arm64-parity/arithmetic-zero-selected-apps.txt').write_text(','.join(selected))
core={line.split()[1] for line in changes if line.startswith('core ')}
manifest=json.loads(Path('/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache/manifest.json').read_text())
selected=[w['id'] for w in manifest['workloads'] if w['id'].replace('/','__')+'.bin' in core and w['oracle']['kind']=='exact_u64'][:4]
Path('experiments/arm64-parity/arithmetic-zero-selected-core.txt').write_text('\n'.join(selected)+'\n')
SELECT
apps=$(cat experiments/arm64-parity/arithmetic-zero-selected-apps.txt)
if [ -n "$apps" ];then
 go build -tags wago_guardpage -o /tmp/paired-arithmetic-zero ./experiments/arm64-parity/paired
 for phase in compile exec;do
  /tmp/paired-arithmetic-zero -option arithmetic-zero-test -workloads "$apps" -phase "$phase" -rounds 8 -budget 200ms > "experiments/arm64-parity/arithmetic-zero-paired-$phase.jsonl" 2>&1
 done
fi
index=0
for state in 1 0 0 1;do
 while IFS= read -r id;do
  [ -n "$id" ] || continue
  export WAGO_PARITY_CORE_ID="$id"
  name=$(printf '%s' "$id" | tr / _)
  WAGO_ARM64_EXPERIMENT_ARITHMETIC_ZERO="$state" /tmp/parity-arithmetic-zero.test -test.run=^$ -test.bench='BenchmarkCachedContract/(Compile|Exec)$' -test.benchtime=400ms -test.count=2 > "experiments/arm64-parity/arithmetic-zero-$name-$index-$state.txt" 2>&1
 done < experiments/arm64-parity/arithmetic-zero-selected-core.txt
 index=$((index+1))
done
