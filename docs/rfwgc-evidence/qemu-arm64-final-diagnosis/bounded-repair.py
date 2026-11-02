#!/usr/bin/env python3
"""Reviewable mapped-only rerun of hashed fail-stop binaries; new output required.
No ledger edits, no broad qualification, no source changes. Cache must be retained.
"""
import argparse,hashlib,json,os,signal,subprocess
from pathlib import Path
E=Path(__file__).resolve().parent
p=argparse.ArgumentParser();p.add_argument('--out',required=True);a=p.parse_args()
out=Path(a.out).resolve();out.mkdir(parents=True,exist_ok=False)
m=json.loads((E/'provenance.json').read_text())
def check(path,want):
 if hashlib.sha256(Path(path).read_bytes()).hexdigest()!=want:raise ValueError('hash mismatch: '+path)
check(m['qemu'],m['qemu_sha256'])
for b in m['binaries'].values():check(b['path'],b['sha256'])
# Exactly the proven map. Guest signal64 remains signal64, with full sigval.
rows=[]
for mode,b in m['binaries'].items():
 for name in ['TestRRRecheckStorageOnlyReturnBehindDynamicReader','TestSharedEntryGuardCancellationAndGC']:
  argv=[m['qemu'],'-cpu','max','-t',m['signal_map'],b['path'],'-test.run=^'+name+'$','-test.v','-test.count=1','-test.timeout=30s']
  log=out/(mode+'-'+name+'.log')
  with log.open('wb') as f:
   child=subprocess.Popen(argv,cwd=Path(m['export'])/'src/wago',env=m['environment'],stdout=f,stderr=subprocess.STDOUT,start_new_session=True)
   try:rc=child.wait(timeout=45)
   except subprocess.TimeoutExpired:
    os.killpg(child.pid,signal.SIGKILL);child.wait();rc='TIMEOUT-KILLED'
  text=log.read_text();ok=rc==0 and '--- PASS: '+name in text and 'SKIP' not in text and 'FAIL' not in text
  rows.append(dict(mode=mode,name=name,argv=argv,exit=rc,pass_gate=ok,log_sha256=hashlib.sha256(log.read_bytes()).hexdigest()))
  (out/'results.json').write_text(json.dumps(rows,indent=2)+'\n')
  if not ok:raise SystemExit('STOP: failed gate; retained result and no continuation')
