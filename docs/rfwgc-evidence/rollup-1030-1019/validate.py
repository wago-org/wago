#!/usr/bin/env python3
"""Export pinned tracked sources plus owned collector edits; correctness only."""
import hashlib, json, os, pathlib, shutil, subprocess, sys, tarfile, tempfile
root = pathlib.Path(__file__).resolve().parents[3]
phase = sys.argv[1]
out = root / 'docs/rfwgc-evidence/rollup-1030-1019' / phase
out.mkdir(exist_ok=True)
head = subprocess.check_output(['git','rev-parse','HEAD'], cwd=root, text=True).strip()
scratch = pathlib.Path(tempfile.mkdtemp(prefix='wago-rollup-'+phase+'-'))
archive = scratch / 'source.tar'
subprocess.run(['git','archive','-o',str(archive),head], cwd=root, check=True)
source = scratch / 'source'
source.mkdir()
with tarfile.open(archive) as tar: tar.extractall(source, filter='data')
owned = root / 'src/core/runtime/gc/native'
shutil.copytree(owned, source / owned.relative_to(root), dirs_exist_ok=True)
hashes = {str(p.relative_to(root)):hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted(owned.glob('*.go'))}
(out/'source.json').write_text(json.dumps({'archive_head':head,'collector_sha256':hashes}, indent=2)+'\n')
env = dict(os.environ, GOMAXPROCS='4', GOROOT='/home/dev/.local/share/mise/installs/go/1.27.1', GOTOOLCHAIN='local')
results = []
try:
 for mode in ['ordinary','wagodebug','wago_regalloccheck']:
  flags = [] if mode == 'ordinary' else ['-tags='+mode]
  for suite, package, selector, race in [
   ('collector','./src/core/runtime/gc/...','.',False),
   ('backend','./src/core/compiler/backend/railshot/amd64','GC|Collector|NativeAlloc|Eden',False),
   ('guest','./src/wago','^TestGC',False),
   ('race','./src/core/runtime/gc/native','.',True)]:
   command = ['/home/dev/.local/share/mise/installs/go/1.27.1/bin/go','test','-p=4',*flags,package,'-run',selector,'-count=1','-v','-timeout=10m']
   if race: command.insert(2,'-race')
   with (out/(mode+'-'+suite+'.txt')).open('w') as log:
    result = subprocess.run(command, cwd=source, env=env, stdout=log, stderr=subprocess.STDOUT)
   # Incidental runner durations are not retained as measurement evidence.
   logpath = out/(mode+'-'+suite+'.txt')
   import re
   logpath.write_text(re.sub(r' \([0-9.]+s\)', '', re.sub(r'\t[0-9.]+s(?=\n)', '', logpath.read_text())))
   row = dict(mode=mode,suite=suite,command=command,exit=result.returncode)
   results.append(row)
   (out/'results.json').write_text(json.dumps(results,indent=2)+'\n')
   print(mode,suite,'exit='+str(result.returncode), flush=True)
finally:
 shutil.rmtree(scratch)
sys.exit(any(row['exit'] for row in results))
