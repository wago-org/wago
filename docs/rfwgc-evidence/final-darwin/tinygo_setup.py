#!/usr/bin/env python3
"""Optional reviewed Darwin TinyGo setup; no download, execution, or runtime port.
Requires a separately provisioned native TinyGo 0.42.0 and its approved binary hash.
Outputs post-patch source hashes for reviewer comparison before native test activation.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import subprocess
from contracts import SPEC, digest
from identity import validate
from run import capture, write

p = argparse.ArgumentParser(description=__doc__)
p.add_argument('source', type=Path)
p.add_argument('output', type=Path)
p.add_argument('--tinygo-sha256', required=True)
a = p.parse_args()
if (platform.system(), platform.machine()) != ('Darwin', 'arm64'):
    p.error('requires actual Darwin arm64')
validate(os.environ.get('QUALIFICATION_SOURCE', ''), os.environ.get('QUALIFICATION_DRIVER', ''))
root = a.source.resolve()
if capture(['git','rev-parse','HEAD'],root) != SPEC['source'] or capture(['git','status','--porcelain'],root):
    p.error('requires clean pinned source')
for name, sha in SPEC['source_files'].items():
    if digest(root/name) != sha:
        p.error('source hash mismatch: '+name)
import shutil
binary = Path(shutil.which('tinygo') or 'missing').resolve()
if digest(binary) != a.tinygo_sha256:
    p.error('TinyGo binary digest differs from separately reviewed native artifact')
version = capture([str(binary),'version'],root)
if not version.startswith('tinygo version 0.42.0 darwin/arm64'):
    p.error('wrong TinyGo native version')
if subprocess.run(['sysctl','-in','sysctl.proc_translated'],capture_output=True,text=True).stdout.strip() == '1':
    p.error('Rosetta is unsupported')
a.output.mkdir(parents=True,exist_ok=False)
subprocess.run(['bash','scripts/patch-tinygo-042.sh'],cwd=root,check=True)
tree = Path(capture([str(binary),'env','TINYGOROOT'],root))
files = ['src/internal/task/task_threads.c','src/internal/task/task_threads.go','src/runtime/gc_stack_threads.go','src/internal/task/wago_foreign_stack.go']
write(a.output/'tinygo-setup.json', {'source':SPEC['source'],'version':version,'binary_sha256':digest(binary),'patched_files':{name:digest(tree/name) for name in files},'execution':'NOT_RUN; requires review and all-three-mode evacuation/foreign-stack/conversion census'})
