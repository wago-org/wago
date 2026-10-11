#!/bin/sh
set -eu
export WAGO_SHARED_SCALAR=0
export WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
unset WAGO_ARM64_EXPERIMENT_BOUNDED_BRANCH_INDEX WAGO_ARM64_NO_BOUNDED_BRANCH_INDEX
root=/Users/work/Code/Wago/wago/experiments/arm64-parity
cd /tmp/wago-bounded-dispatch-37174169
gofmt -w src/core/compiler/backend/railshot/arm64/bounded_branch_index.go src/core/compiler/backend/railshot/arm64/policy_arm64_test.go src/core/compiler/optimization/catalog.go
go test -tags wago_codegenstats,wago_regalloccheck,wago_guardpage ./src/core/compiler/backend/railshot/arm64 ./src/core/compiler/optimization ./src/core/encoder/arm64 > "$root/bounded-branch-index-default-diagnostics.txt" 2>&1
go test -c -tags wago_guardpage -o /tmp/parity-bounded-branch-index-default-signal.test ./experiments/arm64-parity
for state in on off; do
    if [ "$state" = off ]; then export WAGO_ARM64_NO_BOUNDED_BRANCH_INDEX=1; fi
    WAGO_PARITY_CODE_DIR=/tmp/bounded-branch-index-default-$state-app /tmp/parity-bounded-branch-index-default-signal.test -test.run '^$' -test.bench '^BenchmarkParity/language-register-vm/Exec$' -test.benchtime=1x -test.count=1 > "$root/bounded-branch-index-default-$state.txt" 2>&1
done
python3 - <<'PY'
from pathlib import Path
root=Path('/Users/work/Code/Wago/wago/experiments/arm64-parity')
for state,option in [('on',1),('off',0)]:
    actual=Path('/tmp/bounded-branch-index-default-'+state+'-app/language-register-vm.bin').read_bytes()
    expected=Path('/tmp/bounded-branch-index-images/language-register-vm-'+str(option)+'.bin').read_bytes()
    assert actual==expected,state
(root/'bounded-branch-index-default-native-proof.txt').write_text('Default ON and NO_BOUNDED_BRANCH_INDEX=1 OFF register-VM images match the exact qualified/timed inventories; both1x exact oracles pass. Diagnostic per-compilation rollback checks also pass.\n')
PY
