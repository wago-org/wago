#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_INTERVAL_CALL_REGIONS=1
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 ./src/core/compiler/optimization > experiments/arm64-parity/interval-call-skip-enabled-tests.txt 2>&1
go test -tags wago_guardpage -c -o /tmp/parity-interval-call-skip.test ./experiments/arm64-parity
WAGO_PARITY_CORE_CODE_DIR=/tmp/interval-call-skip-core /tmp/parity-interval-call-skip.test -test.run='^TestCachedCoreOracles$' > experiments/arm64-parity/interval-call-skip-core.txt 2>&1
WAGO_PARITY_CODE_DIR=/tmp/interval-call-skip-app /tmp/parity-interval-call-skip.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > experiments/arm64-parity/interval-call-skip-app.txt 2>&1
python3 - <<'CHECK'
from pathlib import Path
lines=[]
for kind in ['core','app']:
 for p in sorted(Path('/tmp/interval-call-skip-'+kind).glob('*.bin')):
  before=Path('/tmp/scoped-loop-retained-'+kind)/p.name
  if p.read_bytes()!=before.read_bytes():lines.append(f'{kind} {p.name} {before.stat().st_size} {p.stat().st_size}')
Path('experiments/arm64-parity/interval-call-skip-native-changes.txt').write_text('\n'.join(lines)+'\n')
CHECK
for image in /tmp/interval-call-skip-core/*.bin; do cmp "$image" "/tmp/interval-call-next-use-core/${image##*/}"; done
for image in /tmp/interval-call-skip-app/*.bin; do cmp "$image" "/tmp/interval-call-next-use-app/${image##*/}"; done
index=0
for variant in old new new old; do
 binary=/tmp/parity-interval-call-next-use.test
 if [ "$variant" = new ]; then binary=/tmp/parity-interval-call-skip.test; fi
 for id in wago/fastfloat/decimal-parse wago/kissfft/complex-roundtrip; do
  export WAGO_PARITY_CORE_ID="$id"
  name=$(printf '%s' "$id" | tr / _)
  "$binary" -test.run=^$ -test.bench='BenchmarkCachedContract/Compile$' -test.benchtime=600ms -test.count=3 > "experiments/arm64-parity/interval-call-skip-$name-$index-$variant.txt" 2>&1
 done
 index=$((index+1))
done
go build -tags wago_guardpage -o /tmp/paired-interval-call-skip ./experiments/arm64-parity/paired
WAGO_ARM64_EXPERIMENT_INTERVAL_CALL_REGIONS=0 /tmp/paired-interval-call-skip -core-compile -corpus "$WAGO_PARITY_CACHE" -option interval-call-regions -workloads decimal-parse,complex-roundtrip -phase compile -rounds 10 -budget 250ms > experiments/arm64-parity/interval-call-skip-compile-pairs.jsonl 2>&1
