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
WAGO_PARITY_CORE_CODE_DIR=/tmp/constant-shift-core-before /tmp/parity-constant-shift-core-before.test -test.run='^TestCachedCoreOracles$' -test.v > experiments/arm64-parity/constant-shift-core-before-oracles.txt 2>&1
WAGO_PARITY_CORE_CODE_DIR=/tmp/constant-shift-core-after /tmp/parity-constant-shift-core-after.test -test.run='^TestCachedCoreOracles$' -test.v > experiments/arm64-parity/constant-shift-core-after-oracles.txt 2>&1
go test ./experiments/arm64-parity -run '^TestCachedCoreOracles$' -v > experiments/arm64-parity/cached-core-explicit-oracles.txt 2>&1
python3 - <<'SCOPE'
from pathlib import Path
files=sorted(Path('/tmp/constant-shift-core-after').glob('*.bin'))
if len(files)!=46:raise SystemExit('incomplete core images')
changed=[p.stem for p in files if p.read_bytes()!=(Path('/tmp/constant-shift-core-before')/p.name).read_bytes()]
Path('experiments/arm64-parity/constant-shift-core-changed.txt').write_text('\n'.join(changed)+'\n')
SCOPE
