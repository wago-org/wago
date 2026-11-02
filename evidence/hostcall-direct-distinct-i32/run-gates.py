#!/usr/bin/env python3
"""Correctness-only candidate gates; owns and removes only its temporary builds."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import sys

root = Path(__file__).resolve().parents[2]
out = Path(__file__).resolve().parent
commands = [
 ('focused', '.', "go test -count=1 -v -run 'TestNative(Leaf|ScalarLeaf)' ./src/wago ./src/core/runtime"),
 ('build', '.', 'go build ./...'),
 ('build-runtime', '.', 'go build -tags wago_runtime ./cli/...'),
 ('ordinary', '.', 'go test -count=1 ./...'),
 ('checked', '.', 'go test -tags=wago_regalloccheck -count=1 ./...'),
 ('runtime', '.', 'go test -count=1 -tags wago_runtime ./cli/...'),
 ('runtime-checked', '.', 'go test -count=1 -tags wago_regalloccheck,wago_runtime ./cli/...'),
 ('installer-checked', 'cli/wago-installer', 'go test -tags=wago_regalloccheck -count=1 ./...'),
 ('bench-module-checked', 'bench', "go test -tags=wago_regalloccheck -count=1 -skip '^(TestCorpus|TestCorpusSemanticExec|TestApplicationCorpusRuns|TestCatalog.*|TestValidateCorpusModule.*)$' ./..."),
 ('debug', '.', 'go test -tags=wagodebug -count=1 ./src/wago ./src/core/runtime'),
 ('race', '.', 'go test -race -count=1 ./src/wago ./src/core/runtime ./tests/integration/runtimeconcurrency'),
 ('profile', '.', "go test -tags=wago_profile -count=1 -v -run '^TestNativeLeafDistinct' ./src/wago"),
]
prefix = 'qualified-' if '--qualified' in sys.argv else ('harness-' if '--harness' in sys.argv else '')
base_env = dict(os.environ)
if prefix:
    go_root = subprocess.check_output(['go', 'env', 'GOROOT'], cwd=root, text=True).strip()
    tooling = json.loads((out / 'tooling.json').read_text())
    base_env.update(GOROOT=go_root, DOTNET_SYSTEM_GLOBALIZATION_INVARIANT='1')
    base_env['PATH'] = tooling['path'] + ':' + go_root + '/bin:' + base_env['PATH']
    if prefix == 'qualified-':
        base_env['WAGO_SPEC_INTERPRETER'] = tooling['interpreter']
        base_env['WAGO_SPEC_INTERPRETER_REVISION'] = tooling['interpreter_build']['source_revision']
results = []
for name, cwd, command in commands:
    head = subprocess.check_output(['git','rev-parse','HEAD'],cwd=root,text=True).strip()
    tmp = tempfile.mkdtemp(prefix='wago-direct-gate-')
    env = dict(base_env, GOTMPDIR=tmp)
    print('START', name, flush=True)
    try:
        with (out / (prefix + name + '.log')).open('w') as log:
            result = subprocess.run(command, shell=True, cwd=root / cwd, env=env, stdout=log, stderr=subprocess.STDOUT)
        results.append(dict(name=name, cwd=cwd, command=command, head=head, exit_code=result.returncode, harness_environment={k:env[k] for k in ['GOROOT','DOTNET_SYSTEM_GLOBALIZATION_INVARIANT','PATH','WAGO_SPEC_INTERPRETER','WAGO_SPEC_INTERPRETER_REVISION'] if k in env}))
        (out / (prefix + 'gates.json')).write_text(json.dumps(results, indent=2)+'\n')
        print('END', name, result.returncode, flush=True)
    finally:
        shutil.rmtree(tmp)

raise SystemExit(1 if any(result['exit_code'] != 0 for result in results) else 0)
