#!/usr/bin/env python3
"""Require complete immutable correctness receipts; never reinterpret skips."""
import hashlib,json,pathlib,subprocess
root=pathlib.Path.cwd();out=root/'docs/rr3-retirement-repair'
pin='5847df30f1af15d10b743d90a24b1429b290d358'
required=['TestRRRecheckStorageOnlyReturnBehindDynamicReader','TestRRRecheckCanceledAdmissionBehindDynamicReader','TestRRRecheckSharedMemoryHandoff','TestRRRetirementLastPinAndRevival','TestRRRetirementCallbackAndExitWakeups','TestRRRetirementBorrowedRootLatePublisher','TestRRRetirementCanceledPartialLastPins','TestRRRetirementCallbackRefreshBehindReader','TestDesignRecheckForeignRelayAfterLatePublisher','TestDesignRecheckStorageOnlyCallTopologyWriter','TestDesignRecheckCallbackChangesPublisherAuthority','TestDesignRecheckNestedSameRuntimeClear','TestDesignRecheckNestedPublish','TestDesignInvocationSnapshotCancellation','TestDesignInvocationSnapshotLatePublisher','TestDesignInvocationSnapshotRetirement','TestDesignInvocationSnapshotAuthenticatedDispatch','TestDesignInvocationSnapshotCallbackTrap']
expected={}
for entry in subprocess.check_output(['git','ls-tree','-rz',pin,'--','src']).split(b'\0'):
 if not entry:continue
 metadata,path=entry.split(b'\t',1);mode,kind,oid=metadata.split()
 if kind==b'blob':expected[path.decode()]=oid.decode()
summary={'source_pin':pin,'qualified':True,'failed_commands':[],'lanes':{},'source_integrity':{},'limitations':['Darwin ARM64 and Windows AMD64: build only; execution NOT_RUN in this worker','TinyGo: supported conversion smoke only, not host opaque receipts','Race: scoped focused selection, not full-package race','No benchmark, performance, full conformance campaign, self-approval or final combined-head claim']}
for lane,count in [('integrated',18),('targets',14),('nonhost',6),('tinygo',3)]:
 d=out/(lane+'-5847df30f');rows=json.loads((d/'results.json').read_text());assert len(rows)==count,(lane,len(rows))
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
  label=r.get('label',r.get('mode'));text=(d/(label+'.log')).read_text();lines=text.splitlines();passes=[x.split()[2] for x in lines if x.startswith('--- PASS:')];skips=[x for x in lines if '--- SKIP:' in x];failures=[x.split()[2] for x in lines if x.startswith('--- FAIL:')]
  if r['exit']!=0 or failures or 'DATA RACE' in text:
   summary['qualified']=False;summary['failed_commands'].append({'lane':lane,'label':label,'exit':r['exit'],'failed_tests':failures})
  else: assert not failures and 'DATA RACE' not in text,(lane,label)
  if lane=='integrated' and (label.endswith('-focused') or label.endswith('-race')) or lane=='targets' and label.startswith('arm64-') and label.endswith('-guest'):
   assert all(x in passes+failures for x in required),(label,set(required)-set(passes+failures));assert not skips,(label,skips)
   if r['exit']==0:assert all(x in passes for x in required),(label,set(required)-set(passes))
  if lane=='tinygo':assert len(passes)==5 and not skips,(label,passes,skips)
  if lane=='targets' and label.endswith('-guest'):
   assert not skips and 'TestPlatformExecutionArchitecture' in passes
   assert len(passes)+len(failures)==(75 if label.startswith('arm64-') else 48),(label,len(passes),failures)
  if lane=='integrated' and (label.endswith('-focused') or label.endswith('-race')):assert len(passes)==100 and not skips,(label,len(passes))
  cells.append({'label':label,'exit':r['exit'],'top_level_passes':len(passes),'recorded_skips':skips,'failed_tests':failures})
 summary['lanes'][lane]=cells
(out/'summary.json').write_text(json.dumps(summary,indent=2)+'\n')
print(json.dumps({lane:[(r['label'],r['top_level_passes'],len(r['recorded_skips'])) for r in rows] for lane,rows in summary['lanes'].items()},indent=2))

raise SystemExit(0 if summary['qualified'] else 1)
