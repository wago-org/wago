#!/usr/bin/env python3
"""Ten fixed alternating pairs for new concerns from the full-suite screen."""
from importlib.util import spec_from_file_location, module_from_spec
from pathlib import Path

spec = spec_from_file_location('focused', Path(__file__).with_name('run-focused.py'))
focused = module_from_spec(spec)
spec.loader.exec_module(focused)
cases = [
    ('^Benchmark(CommandExec|WazeroCommandExec)$/^(sightglass-shootout-base64|wren-modulo)$', 'sightglass-shootout-base64,wren-modulo'),
    ('^BenchmarkExec$/^(polybench-doitgen|kissfft|polybench-symm)\\.', 'polybench-doitgen,kissfft,polybench-symm'),
    ('^BenchmarkDecode$/^utf-as$', 'utf-as'),
    ('^BenchmarkCompile$/^embench-matmult-int$', 'embench-matmult-int'),
    ('^BenchmarkExecFibLoop_wago$', 'tiny'),
]
for sample in range(1, 11):
    for label in (['baseline', 'candidate'] if sample % 2 else ['candidate', 'baseline']):
        for pattern, corpus in cases:
            focused.run(label, 'full-flags', 'diagnostic', ['-test.bench', pattern, '-test.benchtime', '200ms', '-wago.corpus', corpus], sample)
