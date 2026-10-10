#!/bin/sh
set -eu
python3 - <<'APPLY'
from pathlib import Path
import hashlib
p=Path('src/core/compiler/backend/railshot/arm64/interval_region.go')
assert hashlib.sha256(p.read_bytes()).hexdigest() == '38d8b2e65499a72501573c0c79d3223e93401f62287fa40b6e2f858f68b2de65', 'source changed before packed event trial'
p.write_text(Path('experiments/arm64-parity/interval-event-packed.go.txt').read_text())
Path('src/core/compiler/backend/railshot/arm64/interval_event_bounds_arm64_test.go').write_text(Path('experiments/arm64-parity/interval_event_bounds_arm64_test.go.txt').read_text())
APPLY
gofmt -w src/core/compiler/backend/railshot/arm64/interval_region.go src/core/compiler/backend/railshot/arm64/interval_event_bounds_arm64_test.go
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_INTERVAL_CALL_REGIONS=1
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 ./src/core/compiler/optimization > experiments/arm64-parity/interval-call-packed-enabled-tests.txt 2>&1
go test -tags wago_guardpage -c -o /tmp/parity-interval-call-packed.test ./experiments/arm64-parity
WAGO_PARITY_CORE_CODE_DIR=/tmp/interval-call-packed-core /tmp/parity-interval-call-packed.test -test.run='^TestCachedCoreOracles$' > experiments/arm64-parity/interval-call-packed-core.txt 2>&1
WAGO_PARITY_CODE_DIR=/tmp/interval-call-packed-app /tmp/parity-interval-call-packed.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > experiments/arm64-parity/interval-call-packed-app.txt 2>&1
python3 - <<'CHECK'
from pathlib import Path
lines=[]
for kind in ['core','app']:
 for p in sorted(Path('/tmp/interval-call-packed-'+kind).glob('*.bin')):
  before=Path('/tmp/scoped-loop-retained-'+kind)/p.name
  if p.read_bytes()!=before.read_bytes():lines.append(f'{kind} {p.name} {before.stat().st_size} {p.stat().st_size}')
Path('experiments/arm64-parity/interval-call-packed-native-changes.txt').write_text('\n'.join(lines)+'\n')
CHECK
for image in /tmp/interval-call-packed-core/*.bin; do cmp "$image" "/tmp/interval-call-inline-core/${image##*/}"; done
for image in /tmp/interval-call-packed-app/*.bin; do cmp "$image" "/tmp/interval-call-inline-app/${image##*/}"; done
index=0
for variant in old new new old; do
 binary=/tmp/parity-interval-call-inline.test
 if [ "$variant" = new ]; then binary=/tmp/parity-interval-call-packed.test; fi
 for id in wago/fastfloat/decimal-parse wago/kissfft/complex-roundtrip; do
  export WAGO_PARITY_CORE_ID="$id"
  name=$(printf '%s' "$id" | tr / _)
  "$binary" -test.run=^$ -test.bench='BenchmarkCachedContract/Compile$' -test.benchtime=600ms -test.count=3 > "experiments/arm64-parity/interval-call-packed-$name-$index-$variant.txt" 2>&1
 done
 index=$((index+1))
done
go build -tags wago_guardpage -o /tmp/paired-interval-call-packed ./experiments/arm64-parity/paired
WAGO_ARM64_EXPERIMENT_INTERVAL_CALL_REGIONS=0 /tmp/paired-interval-call-packed -core-compile -corpus "$WAGO_PARITY_CACHE" -option interval-call-regions -workloads decimal-parse,complex-roundtrip -phase compile -rounds 10 -budget 250ms > experiments/arm64-parity/interval-call-packed-compile-pairs.jsonl 2>&1
