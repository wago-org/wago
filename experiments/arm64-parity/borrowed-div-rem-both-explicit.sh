#!/bin/sh
set -eu
cd /tmp/wago-borrowed-div-rem-isolated-20261010
export GOWORK=off WAGO_SHARED_SCALAR=0 WAGO_ARM64_EXPERIMENT_LEAF_SCOPED_CONST=0 WAGO_ARM64_EXPERIMENT_BORROWED_DIV_REM=1
export WAGO_PARITY_CACHE=/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache WAGO_PARITY_CORPUS=/Users/work/Code/Web/wasm.fyi/corpora/applications
python3 - <<'PY'
from pathlib import Path
import subprocess
root=Path('/Users/work/Code/Wago/wago/experiments/arm64-parity')
source=Path('src/core/compiler/backend/railshot/arm64/borrowed_div_rem.go')
original=source.read_bytes()
concrete=(root/'borrowed-div-rem-prototype/borrowed_div_rem_concrete_qualified.go.disabled').read_bytes()
try:
 for variant,code in [('owned-result',original),('concrete',concrete)]:
  source.write_bytes(code)
  binary=f'/tmp/parity-borrowed-div-rem-{variant}-explicit.test'
  subprocess.run(['go','test','-c','-o',binary,'./experiments/arm64-parity'],check=True)
  cases=[('core',['-test.run=^TestCachedCoreOracles$']),('app',['-test.run=^$','-test.bench=BenchmarkParity/.*/Exec$','-test.benchtime=1x'])]
  for kind,args in cases:
   with (root/f'borrowed-div-rem-{variant}-{kind}-explicit.txt').open('w') as output:
    subprocess.run([binary,*args],check=True,stdout=output,stderr=subprocess.STDOUT)
  with (root/f'borrowed-div-rem-{variant}-plain-tests.txt').open('w') as output:
   subprocess.run(['go','test','./src/core/compiler/backend/railshot/arm64'],check=True,stdout=output,stderr=subprocess.STDOUT)
finally:
 source.write_bytes(original)
print('PASS: both variants explicit corpus oracles and ordinary backend tests; isolated source restored')
PY
