#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0 WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache
hint=src/core/compiler/backend/railshot/arm64/hints.go
cp "$hint" /tmp/constant-shift-hints-current.go
trap 'cp /tmp/constant-shift-hints-current.go "$hint"' EXIT INT TERM
python3 - <<'BASE'
from pathlib import Path
p=Path('src/core/compiler/backend/railshot/arm64/hints.go');s=p.read_text()
start=s.index('\tcase 0x6c, 0x7e: // Power-of-two multiplies')
end=s.index('\tcase 0x6a, 0x6b',start)
p.write_text(s[:start]+'\tcase 0x6c, 0x7e: // i32/i64.mul have no immediate form.\n\t\treturn true\n'+s[end:])
BASE
go test -tags wago_guardpage -c -o /tmp/parity-constant-shift-core-before.test ./experiments/arm64-parity
cp /tmp/constant-shift-hints-current.go "$hint"
go test -tags wago_guardpage -c -o /tmp/parity-constant-shift-core-after.test ./experiments/arm64-parity
export WAGO_PARITY_CORE_ID=wago/linked_list/sum
export WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
index=0
for variant in before after after before; do
 binary=/tmp/parity-constant-shift-core-before.test
 if [ "$variant" = after ]; then binary=/tmp/parity-constant-shift-core-after.test; fi
 "$binary" -test.run=^$ -test.bench='BenchmarkCachedSimple/(Compile|Exec)$' -test.benchtime=400ms -test.count=3 > "experiments/arm64-parity/constant-shift-linked-$index-$variant.txt" 2>&1
 "$binary" -test.run=^$ -test.bench='BenchmarkParity/(vision-components|audio-fir|search-aho-corasick)/Compile$' -test.benchtime=300ms -test.count=2 > "experiments/arm64-parity/constant-shift-compile-$index-$variant.txt" 2>&1
 index=$((index+1))
done
go test -c -o /tmp/parity-constant-shift-explicit.test ./experiments/arm64-parity
/tmp/parity-constant-shift-explicit.test -test.run=^$ -test.bench='BenchmarkParity/.*/Exec$' -test.benchtime=1x > experiments/arm64-parity/constant-shift-explicit-oracles.txt 2>&1
