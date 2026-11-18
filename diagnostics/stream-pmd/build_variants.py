#!/usr/bin/env python3
"""Explicit opt-in reproduction recipe; not a project default or benchmark runner.
Requires the matching full adapter and a modfile resolving Wago to --source.
Run static/API checks before executing the produced adapters. Linux/amd64 only.
"""
import argparse, hashlib, json, os, pathlib, subprocess
p = argparse.ArgumentParser()
for name in ('go', 'source', 'adapter', 'modfile', 'out'):
    p.add_argument('--' + name, required=True, type=pathlib.Path)
p.add_argument('--cpu', required=True, type=int)
p.add_argument('--execute-builds', action='store_true')
a = p.parse_args()
if not a.execute_builds:
    p.error('Explicit --execute-builds is required; no default launch')
base = 'b5ab03f924e6b74ee3b558e34ad9cb32c9026acd'
def run(cmd, **kw):
    return subprocess.check_output(list(map(str, cmd)), text=True, **kw).strip()
def source_state():
    return [run(['git', '-C', a.source, 'rev-parse', 'HEAD']),
            run(['git', '-C', a.source, 'status', '--porcelain'])]
assert source_state() == [base, ''], 'Use a clean exact frozen source checkout'
assert run([a.go, 'version']) == 'go version go1.27.1 linux/amd64'
env = os.environ.copy()
env.update(GOWORK='off', GOTOOLCHAIN='local', GOFLAGS='', GOMAXPROCS='1')
for k in env:
    if k.startswith('WAGO_EXPERIMENTAL_'):
        env[k] = '0'
mod = '-modfile=' + str(a.modfile.resolve())
resolved = run([a.go, 'list', '-mod=mod', mod, '-m', '-f', '{{.Dir}}',
                'github.com/wago-org/wago'], cwd=a.adapter, env=env)
assert pathlib.Path(resolved).resolve() == a.source.resolve()
a.out.mkdir(parents=True, exist_ok=True)
receipt = {'source': base, 'scope': 'Static internal ELF experiment; not performance qualification', 'builds': []}
for arm, address in [('baseline', '0x401000'), ('candidate', '0x402000'),
                     ('aligned-control', '0x601000')]:
    binary = (a.out / arm).resolve()
    command = ['taskset', '-c', str(a.cpu), str(a.go.resolve()), 'build', '-p', '1',
               '-pgo=off', '-trimpath', '-buildvcs=false', '-mod=mod', mod,
               '-ldflags=-R 4096 -T ' + address + ' -X main.sourceRevision=' + base,
               '-o', str(binary), '.']
    subprocess.run(command, cwd=a.adapter, env=env, check=True)
    receipt['builds'].append({'arm': arm, 'T': address, 'R': 4096,
                             'sha256': hashlib.sha256(binary.read_bytes()).hexdigest(),
                             'version_m': run([a.go, 'version', '-m', binary])})
    (a.out / 'build-receipt.json').write_text(json.dumps(receipt, indent=2) + '\n')
assert source_state() == [base, ''], 'Source changed during build'
