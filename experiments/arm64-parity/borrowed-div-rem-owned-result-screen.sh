#!/bin/sh
set -eu
cd /tmp/wago-borrowed-div-rem-isolated-20261010
export GOWORK=off WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_LEAF_SCOPED_CONST=0 WAGO_ARM64_EXPERIMENT_BORROWED_DIV_REM=1
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
RESULTS=/Users/work/Code/Wago/wago/experiments/arm64-parity
gofmt -w src/core/compiler/backend/railshot/arm64/borrowed_div_rem.go src/core/compiler/backend/railshot/arm64/borrowed_div_rem_concrete_arm64_test.go src/core/compiler/backend/railshot/arm64/emit.go
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 ./src/core/compiler/optimization > "$RESULTS/borrowed-div-rem-owned-result-tests.txt" 2>&1
go test -tags wago_guardpage -c -o /tmp/parity-borrowed-div-rem-owned-result.test ./experiments/arm64-parity
WAGO_PARITY_CORE_CODE_DIR=/tmp/borrowed-div-rem-owned-result-core /tmp/parity-borrowed-div-rem-owned-result.test -test.run='^TestCachedCoreOracles$' > "$RESULTS/borrowed-div-rem-owned-result-core.txt" 2>&1
WAGO_PARITY_CODE_DIR=/tmp/borrowed-div-rem-owned-result-app /tmp/parity-borrowed-div-rem-owned-result.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > "$RESULTS/borrowed-div-rem-owned-result-app.txt" 2>&1
python3 - <<'PY'
from pathlib import Path
out=[];mitigation=[]
for kind,count in [('core',46),('app',102)]:
 files=list(Path('/tmp/borrowed-div-rem-owned-result-'+kind).glob('*.bin'))
 assert len(files)==count
 for p in sorted(files):
  old=Path('/tmp/common-exit-compare-retained-'+kind)/p.name
  initial=Path('/tmp/borrowed-div-rem-concrete-'+kind)/p.name
  if p.read_bytes()!=old.read_bytes():out.append(f'{kind} {p.name} {old.stat().st_size} {p.stat().st_size}')
  if p.read_bytes()!=initial.read_bytes():mitigation.append(f'{kind} {p.name}')
root=Path('/Users/work/Code/Wago/wago/experiments/arm64-parity')
(root/'borrowed-div-rem-owned-result-native-changes.txt').write_text('\n'.join(out)+'\n')
(root/'borrowed-div-rem-owned-result-mitigation-changes.txt').write_text('\n'.join(mitigation)+'\n')
PY
export WAGO_ARM64_EXPERIMENT_BORROWED_DIV_REM=0
go build -tags wago_guardpage -o /tmp/paired-borrowed-div-rem-owned-result ./experiments/arm64-parity/paired
for phase in compile exec; do
 /tmp/paired-borrowed-div-rem-owned-result -option borrowed-div-rem -workloads numeric-euclidean-gcd,db-btree,language-register-vm,stats-bootstrap,ml-kmeans -phase "$phase" -rounds 8 -budget 300ms > "$RESULTS/borrowed-div-rem-owned-result-paired-$phase.jsonl" 2>&1
done
/tmp/paired-borrowed-div-rem-owned-result -core-compile -corpus "$WAGO_PARITY_CACHE" -option borrowed-div-rem -workloads modular-arithmetic -phase compile -rounds 8 -budget 200ms > "$RESULTS/borrowed-div-rem-owned-result-core-paired-compile.jsonl" 2>&1
export WAGO_ARM64_EXPERIMENT_BORROWED_DIV_REM=1
go build -tags wago_runtime,wago_profile,wago_guardpage -o /tmp/wago-borrowed-div-rem-owned-result-profile ./cli/wago
python3 - <<'PY'
from pathlib import Path
import json,subprocess
base=Path('/Users/work/Code/Web/wasm.fyi/corpora/applications')
w=next(w for w in json.loads((base/'manifest.json').read_text()) if w['id']=='applications/numeric-euclidean-gcd')
assert len(w['args'])==1 and w['oracle']['kind']=='exact_u64'
root=Path('/Users/work/Code/Wago/wago/experiments/arm64-parity')
with (root/'profile-borrowed-div-rem-owned-result-gcd.txt').open('w') as out:
 subprocess.run(['/tmp/wago-borrowed-div-rem-owned-result-profile','profile','record','--module',str(base/w['artifact']),'--export',w['export'],'--args',str(w['args'][0]),'--want',w['oracle']['expected'][0],'--bounds','signals','--mode','prepared','--duration','3s','--backend','samply','--samply','/opt/homebrew/bin/samply','--rate','1000','--include-code','--source-maps','--out',str(root/'profile-borrowed-div-rem-owned-result-gcd')],check=True,stdout=out,stderr=subprocess.STDOUT)
PY
/tmp/wago-profile-tools/bin/python "$RESULTS/sampled_assembly.py" "$RESULTS/profile-borrowed-div-rem-owned-result-gcd" > "$RESULTS/profile-borrowed-div-rem-owned-result-gcd-assembly.txt"
