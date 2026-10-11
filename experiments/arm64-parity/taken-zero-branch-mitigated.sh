#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache
export WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
export WAGO_ARM64_EXPERIMENT_TAKEN_ZERO_BRANCH=1
root=/Users/work/Code/Wago/wago/experiments/arm64-parity
cd /tmp/wago-taken-branch-37174169
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 ./src/core/compiler/optimization ./src/core/encoder/arm64 > "$root/taken-zero-branch-mitigated-diagnostics.txt" 2>&1
go build -tags wago_guardpage -o /tmp/paired-taken-zero-branch-mitigated ./experiments/arm64-parity/paired
go test -c -tags wago_guardpage -o /tmp/parity-taken-zero-branch-mitigated-signal.test ./experiments/arm64-parity
WAGO_PARITY_CORE_CODE_DIR=/tmp/taken-zero-branch-mitigated-core /tmp/parity-taken-zero-branch-mitigated-signal.test -test.run '^TestCachedCoreOracles$' -test.v > "$root/taken-zero-branch-mitigated-core.txt" 2>&1
WAGO_PARITY_CODE_DIR=/tmp/taken-zero-branch-mitigated-app /tmp/parity-taken-zero-branch-mitigated-signal.test -test.run '^$' -test.bench '^BenchmarkParity/.*/Exec$' -test.benchtime=1x -test.count=1 > "$root/taken-zero-branch-mitigated-app.txt" 2>&1
python3 - <<'PY'
from pathlib import Path
count=0
for kind,expected in [('core',46),('app',102)]:
 before=Path('/tmp/taken-zero-branch-on-'+kind)
 after=Path('/tmp/taken-zero-branch-mitigated-'+kind)
 files=list(after.glob('*.bin'))
 assert len(files)==expected
 for p in files:
  assert p.read_bytes()==(before/p.name).read_bytes(),p.name
  count+=1
out=Path('/Users/work/Code/Wago/wago/experiments/arm64-parity/taken-zero-branch-mitigated-native-proof.txt')
out.write_text(f'{count} exact native images byte-identical to the qualified original ON. Candidate preflight affects compiler work only.\n')
PY
/tmp/paired-taken-zero-branch-mitigated -core-compile -phase compile -corpus "$WAGO_PARITY_CACHE" -option taken-zero-branch -workloads modular-arithmetic,decimal-parse,compile-match,complex-roundtrip -rounds 8 -budget 150ms > "$root/taken-zero-branch-mitigated-core-compile.jsonl" 2>&1
/tmp/paired-taken-zero-branch-mitigated -option taken-zero-branch -workloads hardware-boolean-anf,audio-adpcm,files-glob-match,compiler-register-allocation -phase compile -rounds 8 -budget 150ms > "$root/taken-zero-branch-mitigated-app-compile.jsonl" 2>&1
python3 "$root/summarize_paired.py" "$root/taken-zero-branch-mitigated-core-compile.jsonl" "$root/taken-zero-branch-mitigated-app-compile.jsonl" > "$root/taken-zero-branch-mitigated-compile-summary.md"
