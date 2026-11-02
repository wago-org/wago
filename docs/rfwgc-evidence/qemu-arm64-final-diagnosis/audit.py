#!/usr/bin/env python3
"""Offline validation of this diagnosis; does not execute guest binaries."""
import hashlib,json,re,subprocess,gzip,tarfile,platform,difflib
from pathlib import Path
E=Path(__file__).resolve().parent;R=E.parents[2]
def sha(b):return hashlib.sha256(b).hexdigest()
def raw(p):return p.read_bytes() if p.exists() else gzip.decompress(Path(str(p)+'.gz').read_bytes())
meta=json.loads((E/'provenance.json').read_text());S=Path(meta['export']);W=S.parent
rows=json.loads((E/'commands.json').read_text())
for r in rows:assert sha(raw(E/(r['label']+'.log')))==r['log_sha256'],r['label']
manifest=json.loads((E/'source-manifest.json').read_text())
gitfiles={}
for line in subprocess.check_output(['git','ls-tree','-r',meta['pin']],cwd=R,text=True).splitlines():
 left,path=line.split('\t');mode,kind,oid=left.split()
 if kind=='blob' and not path.startswith('docs/'):gitfiles[path]=oid
assert set(manifest)==set(gitfiles),(set(gitfiles)-set(manifest),set(manifest)-set(gitfiles))
for p,h in manifest.items():
 b=(S/p).read_bytes();assert sha(b)==h,p
 assert hashlib.sha1(b'blob '+str(len(b)).encode()+b'\0'+b).hexdigest()==gitfiles[p],p
for mode,b in meta['binaries'].items():assert sha(Path(b['path']).read_bytes())==b['sha256'] and b['elf_machine']==183
result={'source_pin':meta['pin'],'exact_git_blobs':len(manifest),'host':list(platform.uname()),'modes':{},'timer':{}}
for config in ['default','mapped']:
 t=raw(E/('timer-'+config+'.log')).decode();matches=re.findall(r'arch=arm64 clock=1 notify=(\d+) signo=(\d+) tid=\d+ size=64 errno=(\d+)',t)
 assert len(matches)==6
 for n,s,e in matches:assert int(e)==(22 if config=='default' and n!='1' and s=='64' else 0)
 result['timer'][config]=matches
for mode in ['ordinary','wagodebug','wago_regalloccheck']:
 result['modes'][mode]={}
 for kind in ['storage','guard']:
  for cfg in ['default','mapped']:
   label=f'{mode}-{kind}-{cfg}';t=raw(E/(label+'.log')).decode();row=next(r for r in rows if r['label']==label)
   assert row['exit']==(0 if cfg=='mapped' else 86 if kind=='guard' else 1),label
   assert '-test.count=1' in row['argv'] and '-test.timeout=30s' in row['argv']
   assert ('-t' in row['argv'])==(cfg=='mapped');assert 'SKIP' not in t
   if kind=='storage':
    assert t.count('create native deadline timer: invalid argument')==(4 if cfg=='default' else 0)
    if cfg=='mapped':assert t.count('storageReturnBlocked=false secondAdmissionErr=<nil>')==2
   else:
    assert 'timer_create(' not in t
    assert 'rt_sigaction(64,' in t
    sends=[l for l in t.splitlines() if 'rt_tgsigqueueinfo(' in l]
    delivered=re.findall(r'--- 64 \{si_signo=64, si_code=SI_QUEUE,.*?si_sigval=(-?\d+)\}',t)
    if cfg=='default':
     assert sends and all('errno=22' in l for l in sends)
     assert not delivered and 'FAIL-STOP: native loop did not cancel' in t
    else:
     assert sends and delivered and '--- PASS: TestSharedEntryGuardCancellationAndGC' in t
     sent=set(re.findall(r'rt_tgsigqueueinfo\([^\n]*si_sigval=(-?\d+)',t))
     assert set(delivered)<=sent
    result['modes'][mode][cfg+'-guard']={'send_records':len(sends),'delivery_records':len(delivered),'timer_create':0,'exit':row['exit']}
(E/'audit.json').write_text(json.dumps(result,indent=2)+'\n')
print(json.dumps(result,indent=2))
