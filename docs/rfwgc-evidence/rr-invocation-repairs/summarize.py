#!/usr/bin/env python3
"""Require complete immutable correctness receipts; never reinterpret skips."""
import hashlib,json,pathlib,subprocess
root=pathlib.Path.cwd();out=root/'docs/rfwgc-evidence/rr-invocation-repairs'
pin='9ad40bce4a7397dbadf0b0528f2587ed59d7d441'
required=['TestDesignRecheckForeignRelayAfterLatePublisher','TestDesignRecheckStorageOnlyCallTopologyWriter','TestDesignRecheckCallbackChangesPublisherAuthority','TestDesignRecheckNestedSameRuntimeClear','TestDesignRecheckNestedPublish','TestDesignInvocationSnapshotCancellation','TestDesignInvocationSnapshotLatePublisher','TestDesignInvocationSnapshotRetirement','TestDesignInvocationSnapshotAuthenticatedDispatch','TestDesignInvocationSnapshotCallbackTrap']
expected={}
for entry in subprocess.check_output(['git','ls-tree','-rz',pin,'--','src']).split(b'\0'):
 if not entry:continue
 metadata,path=entry.split(b'\t',1);mode,kind,oid=metadata.split()
 if kind==b'blob':expected[path.decode()]=oid.decode()
summary={'source_pin':pin,'lanes':{},'source_integrity':{},'limitations':['Darwin ARM64 and Windows AMD64: build only; execution NOT_RUN in this worker','TinyGo: supported conversion smoke only, not host opaque receipts','Race: scoped focused selection, not full-package race','No benchmark, performance, full conformance campaign, self-approval or final combined-head claim']}
for lane,count in [('integrated',18),('targets',14),('nonhost',6),('tinygo',3)]:
 d=out/(lane+'-9ad40bce4');rows=json.loads((d/'results.json').read_text());assert len(rows)==count,(lane,len(rows));assert all(r['exit']==0 for r in rows),lane
 final=json.loads((d/'driver-final.json').read_text());assert final['driver_unchanged'] and not final['export_changes'],(lane,final)
 meta=json.loads((d/('source.json' if lane=='targets' else 'provenance.json')).read_text());assert meta['pin']==pin
 src=pathlib.Path(meta.get('export',meta.get('source')))
 changed=[]
 for name,oid in expected.items():
  p=src/name
  if not p.is_file():changed.append(name);continue
  data=p.read_bytes();actual=hashlib.sha1(b'blob '+str(len(data)).encode()+b'\0'+data).hexdigest()
  if actual!=oid:changed.append(name)
 assert not changed,(lane,changed)
 summary['source_integrity'][lane]={'src_blobs_checked':len(expected),'changed':changed,'driver_unchanged':True,'export_changes':[]}
 cells=[]
 for r in rows:
  label=r.get('label',r.get('mode'));text=(d/(label+'.log')).read_text();lines=text.splitlines();passes=[x.split()[2] for x in lines if x.startswith('--- PASS:')];skips=[x for x in lines if '--- SKIP:' in x]
  assert 'DATA RACE' not in text and '--- FAIL:' not in text,(lane,label)
  if lane=='integrated' and (label.endswith('-focused') or label.endswith('-race')) or lane=='targets' and label.startswith('arm64-') and label.endswith('-guest'):
   assert all(x in passes for x in required),(label,set(required)-set(passes));assert not skips,(label,skips)
  if lane=='tinygo':assert len(passes)==5 and not skips,(label,passes,skips)
  if lane=='targets' and label.endswith('-guest'):assert not skips and 'TestPlatformExecutionArchitecture' in passes
  cells.append({'label':label,'exit':r['exit'],'top_level_passes':len(passes),'recorded_skips':skips})
 summary['lanes'][lane]=cells
(out/'summary.json').write_text(json.dumps(summary,indent=2)+'\n')
print(json.dumps({lane:[(r['label'],r['top_level_passes'],len(r['recorded_skips'])) for r in rows] for lane,rows in summary['lanes'].items()},indent=2))
