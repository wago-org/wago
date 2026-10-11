#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0
unset WAGO_ARM64_NO_GUARDED_BOOLEAN_XOR WAGO_ARM64_NO_TAKEN_ZERO_BRANCH WAGO_ARM64_NO_INTEGER_SELECT_LOCAL_READ
root=experiments/arm64-parity
/tmp/paired-next-20261011-select-retained -option integer-select-local-read -workloads hardware-boolean-anf,ml-kmeans,geo-polygon-clipping -phase exec -rounds 10 -budget 300ms -code-dir /tmp/next-20261011-select-incremental-images > "$root/next-20261011-select-incremental-exec.jsonl" 2>&1
/tmp/paired-next-20261011-select-retained -option integer-select-local-read -workloads geo-point-in-polygon,hardware-boolean-anf,files-glob-match,audio-adpcm -phase compile -rounds 8 -budget 150ms > "$root/next-20261011-select-incremental-compile.jsonl" 2>&1
python3 "$root/summarize_paired.py" "$root/next-20261011-select-incremental-exec.jsonl" "$root/next-20261011-select-incremental-compile.jsonl" > "$root/next-20261011-select-incremental-summary.md"
python3 - <<'PY'
from pathlib import Path
lines=[]
for name in ['hardware-boolean-anf','ml-kmeans','geo-polygon-clipping']:
 for state,inventory in [(0,'/tmp/next-20261011-on-app'),(1,'/tmp/next-20261011-select-on-app')]:
  assert (Path('/tmp/next-20261011-select-incremental-images')/(name+'-'+str(state)+'.bin')).read_bytes()==(Path(inventory)/(name+'.bin')).read_bytes(),(name,state)
 lines.append(name+': OFF image matches76e52be8; ON matches qualified combined default.')
Path('experiments/arm64-parity/next-20261011-select-incremental-native-proof.txt').write_text('\n'.join(lines)+'\n')
PY
