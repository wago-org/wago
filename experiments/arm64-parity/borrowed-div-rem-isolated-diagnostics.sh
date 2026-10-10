#!/bin/sh
set -eu
cd /tmp/wago-borrowed-div-rem-isolated-20261010
export GOWORK=off WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_LEAF_SCOPED_CONST=0 WAGO_ARM64_EXPERIMENT_BORROWED_DIV_REM=1
gofmt -w src/core/compiler/backend/railshot/arm64/borrowed_div_rem.go src/core/compiler/backend/railshot/arm64/borrowed_div_rem_arm64_test.go src/core/compiler/backend/railshot/arm64/emit.go src/core/compiler/backend/railshot/arm64/knobs.go
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 ./src/core/compiler/optimization > /Users/work/Code/Wago/wago/experiments/arm64-parity/borrowed-div-rem-isolated-tests.txt 2>&1
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
RESULTS=/Users/work/Code/Wago/wago/experiments/arm64-parity
go test -tags wago_guardpage -c -o /tmp/parity-borrowed-div-rem-isolated.test ./experiments/arm64-parity
WAGO_PARITY_CORE_CODE_DIR=/tmp/borrowed-div-rem-isolated-core /tmp/parity-borrowed-div-rem-isolated.test -test.run='^TestCachedCoreOracles$' > "$RESULTS/borrowed-div-rem-isolated-core.txt" 2>&1
WAGO_PARITY_CODE_DIR=/tmp/borrowed-div-rem-isolated-app /tmp/parity-borrowed-div-rem-isolated.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > "$RESULTS/borrowed-div-rem-isolated-app.txt" 2>&1
python3 - <<'PY'
from pathlib import Path
out=[]
for kind,count in [('core',46),('app',102)]:
 files=list(Path('/tmp/borrowed-div-rem-isolated-'+kind).glob('*.bin'))
 assert len(files)==count,(kind,len(files))
 for p in sorted(files):
  old=Path('/tmp/common-exit-compare-retained-'+kind)/p.name
  if p.read_bytes()!=old.read_bytes():out.append(f'{kind} {p.name} {old.stat().st_size} {p.stat().st_size}')
Path('/Users/work/Code/Wago/wago/experiments/arm64-parity/borrowed-div-rem-isolated-native-changes.txt').write_text('\n'.join(out)+'\n')
PY
workloads=$(python3 - <<'PYSELECT'
from pathlib import Path
p=Path('/Users/work/Code/Wago/wago/experiments/arm64-parity/borrowed-div-rem-isolated-native-changes.txt')
changed=[line.split()[1][:-4] for line in p.read_text().splitlines() if line.startswith('app ')]
primary='numeric-euclidean-gcd'
selected=([primary] if primary in changed else [])+[name for name in changed if name!=primary]
selected=selected[:6]
control='compiler-register-allocation'
if control not in selected:selected.append(control)
Path('/Users/work/Code/Wago/wago/experiments/arm64-parity/borrowed-div-rem-isolated-focused-selection.txt').write_text('\n'.join(selected)+'\n')
print(','.join(selected))
PYSELECT
)
export WAGO_ARM64_EXPERIMENT_BORROWED_DIV_REM=0
go build -tags wago_guardpage -o /tmp/paired-borrowed-div-rem-isolated ./experiments/arm64-parity/paired
for phase in compile exec; do
 /tmp/paired-borrowed-div-rem-isolated -option borrowed-div-rem -workloads "$workloads" -phase "$phase" -rounds 8 -budget 200ms > "$RESULTS/borrowed-div-rem-isolated-paired-$phase.jsonl" 2>&1
done
/tmp/paired-borrowed-div-rem-isolated -core-exec -corpus "$WAGO_PARITY_CACHE" -option borrowed-div-rem -workloads modular-arithmetic -phase exec -rounds 8 -budget 200ms > "$RESULTS/borrowed-div-rem-isolated-core-paired-exec.jsonl" 2>&1
