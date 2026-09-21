#!/usr/bin/env python3
"""Ten alternating pairs of instrumented diagnostics; not final timing results."""
from pathlib import Path
import os
import subprocess
ROOT=Path(__file__).resolve().parents[3]
OUT=ROOT/'docs/performance/setup-cleanup'
ENV=dict(os.environ,GOMAXPROCS='16',GOGC='100',GOMEMLIMIT='off',GODEBUG='',WAGO_BOUNDS='signals')
for sample in range(1,11):
 for label in (['baseline','candidate'] if sample%2 else ['candidate','baseline']):
  cmd=['taskset','-c','0-15',str(ROOT/'.tmp/setup-cleanup'/f'{label}-phase.test'),'-test.run','^$','-test.bench','^BenchmarkInstancePhaseProbe$','-test.benchtime','1000x','-test.count','1','-test.benchmem']
  with (OUT/f'phase-probe-{label}.txt').open('a') as out:
   out.write(f'# round {sample}; command {cmd!r}\n');out.flush()
   subprocess.run(cmd,cwd=ROOT/'src/wago',env=ENV,stdout=out,stderr=subprocess.STDOUT,check=True)
