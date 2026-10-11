#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_EQUALITY_COUNTDOWN=1
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 ./src/core/compiler/optimization ./src/core/encoder/arm64 > experiments/arm64-parity/equality-countdown-final-tests.txt 2>&1
for mode in signal explicit; do
 tags=''
 if [ "$mode" = signal ]; then tags=wago_guardpage; fi
 go test -tags "$tags" -c -o "/tmp/parity-equality-countdown-$mode.test" ./experiments/arm64-parity
 WAGO_PARITY_CORE_CODE_DIR="/tmp/equality-countdown-$mode-core" "/tmp/parity-equality-countdown-$mode.test" -test.run='^TestCachedCoreOracles$' > "experiments/arm64-parity/equality-countdown-$mode-core.txt" 2>&1
 WAGO_PARITY_CODE_DIR="/tmp/equality-countdown-$mode-app" "/tmp/parity-equality-countdown-$mode.test" -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > "experiments/arm64-parity/equality-countdown-$mode-app.txt" 2>&1
done
python3 - <<'PY'
from pathlib import Path
out=[]
for kind,count in [('core',46),('app',102)]:
 files=list(Path('/tmp/equality-countdown-signal-'+kind).glob('*.bin'));assert len(files)==count
 for p in sorted(files):
  old=Path('/tmp/leaf-scratch-memory-safe-'+kind)/p.name
  if p.read_bytes()!=old.read_bytes():out.append(f'{kind} {p.name} {old.stat().st_size} {p.stat().st_size}')
Path('experiments/arm64-parity/equality-countdown-final-native-changes.txt').write_text('\n'.join(out)+'\n')
PY
printf 'All 148 exact corpora pass signal and explicit bounds. Backend/catalog/encoder diagnostic tests pass.\n' > experiments/arm64-parity/equality-countdown-final-proof.txt
