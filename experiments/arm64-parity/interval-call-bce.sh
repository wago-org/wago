#!/bin/sh
set -eu
python3 - <<'APPLY'
from pathlib import Path
import hashlib
p=Path('src/core/compiler/backend/railshot/arm64/interval_region.go')
assert hashlib.sha256(p.read_bytes()).hexdigest() == '13d3be94ad2907d419a188fdfe63a9930898a6c45b9071c019a16ca176bc987c', 'source changed before bounds-check trial'
p.write_text(Path('experiments/arm64-parity/interval-event-bounds-native-int.go.txt').read_text())
APPLY
gofmt -w src/core/compiler/backend/railshot/arm64/interval_region.go
export WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_INTERVAL_CALL_REGIONS=1
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 > experiments/arm64-parity/interval-call-bce-tests.txt 2>&1
go test -tags wago_guardpage -c -o /tmp/parity-interval-call-bce.test ./experiments/arm64-parity
go tool objdump -s nextIntervalEvent /tmp/parity-interval-call-floor.test > experiments/arm64-parity/interval-call-bce-before.txt
go tool objdump -s nextIntervalEvent /tmp/parity-interval-call-bce.test > experiments/arm64-parity/interval-call-bce-after.txt
index=0
for variant in old new new old; do
 binary=/tmp/parity-interval-call-floor.test
 if [ "$variant" = new ];then binary=/tmp/parity-interval-call-bce.test;fi
 for id in wago/fastfloat/decimal-parse wago/kissfft/complex-roundtrip; do
  export WAGO_PARITY_CORE_ID="$id"
  name=$(printf '%s' "$id" | tr / _)
  "$binary" -test.run=^$ -test.bench='BenchmarkCachedContract/Compile$' -test.benchtime=600ms -test.count=3 > "experiments/arm64-parity/interval-call-bce-$name-$index-$variant.txt" 2>&1
 done
 index=$((index+1))
done
go build -tags wago_guardpage -o /tmp/paired-interval-call-bce ./experiments/arm64-parity/paired
WAGO_ARM64_EXPERIMENT_INTERVAL_CALL_REGIONS=0 /tmp/paired-interval-call-bce -core-compile -corpus "$WAGO_PARITY_CACHE" -option interval-call-regions -workloads decimal-parse,complex-roundtrip -phase compile -rounds 10 -budget 250ms > experiments/arm64-parity/interval-call-bce-compile-pairs.jsonl 2>&1
for variant in old new; do
 binary=/tmp/paired-interval-call-floor
 if [ "$variant" = new ];then binary=/tmp/paired-interval-call-bce;fi
 WAGO_ARM64_EXPERIMENT_INTERVAL_CALL_REGIONS=0 "$binary" -core-exec -corpus "$WAGO_PARITY_CACHE" -option interval-call-regions -workloads decimal-parse,complex-roundtrip -phase exec -rounds 1 -budget 1ms -code-dir "/tmp/interval-call-bce-$variant" > "experiments/arm64-parity/interval-call-bce-native-$variant.jsonl" 2>&1
done
for image in /tmp/interval-call-bce-new/*.bin;do cmp "$image" "/tmp/interval-call-bce-old/${image##*/}";done
