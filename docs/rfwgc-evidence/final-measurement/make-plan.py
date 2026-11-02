#!/usr/bin/env python3
"""Emit a logical immutable plan and source/input receipts; never execute workloads."""
import hashlib,json,pathlib,subprocess
H=pathlib.Path(__file__).resolve().parent;R=H.parents[2]
SOURCE='5345bd365c13963f1bf60e26bb80789ab9ead4cc'
BASE='a3c08365f531eb3c9003e3acf3e0a822c12c31f4'
def blob(p,pin=SOURCE):return subprocess.check_output(['git','show',pin+':'+p],cwd=R)
def sha(x):return hashlib.sha256(x).hexdigest()
rows=[]
def cell(name,package,selector,a,b,kind='benchmark',scope='native-linux-amd64'):
 rows.append(dict(id=name,package=package,selector=selector,A=a,B=b,kind=kind,target=scope))
def side(pin='port',**env):return dict(pin=pin,env=env)
for package,family,names in [('native','BenchmarkRFWStage3Native',['scalar','graph','low','high','promotion','large']),('guest','BenchmarkRFWStage3Guest',['noGC','scalar','graph']),('guest','BenchmarkRFWStage3Compile',['noGC','scalar','graph'])]:
 for n in names:
  selector='^'+family+'$/^'+n+'$'
  cell('revision/'+family+'/'+n,package,selector,side('baseline',RFW_REGION='Off'),side(RFW_REGION='Off'))
  if 'Compile' not in family:cell('region/'+family+'/'+n,package,selector,side(RFW_REGION='Off'),side(RFW_REGION='On' if n!='noGC' else 'Off'))
for edges in [2,32]:
 selector='^BenchmarkFinalMeasurementGraph$/^'+str(edges)+'$'
 cell('graph-revision/'+str(edges),'native',selector,side('baseline',FM_WORKERS='1',FM_SHIFT='0'),side(FM_WORKERS='1',FM_SHIFT='0'))
 cell('graph-region/'+str(edges),'native',selector,side(FM_WORKERS='1',FM_SHIFT='0'),side(FM_WORKERS='1',FM_SHIFT='12'))
 for w in [2,4,8]:cell(f'graph-workers/{edges}/{w}','native',selector,side(FM_WORKERS='1',FM_SHIFT='12'),side(FM_WORKERS=str(w),FM_SHIFT='12'))
for w in [1,2,4,8]:cell('evacuation/'+str(w),'native','^BenchmarkFinalMeasurementEvacuation$',side(FM_WORKERS=str(w),FM_EVAC='Off'),side(FM_WORKERS=str(w),FM_EVAC='On'))
for name in ['TestR9H2HostContainerPublicGlobal','TestR9H2HostContainerPublicTable','TestR9H2HostContainerCycle','TestR9H2HostImportGlobal','TestR9H2HostImportTable','TestR9H2HostImportBulk','TestR9H2HostImportCycle','TestR9OwnershipWakeupActiveBoundary','TestR9OwnershipWakeupExitCrossing']:
 # Side-specific selectors select actual policy subtests, not inert environment variables.
 a=side();b=side();a['selector']='^'+name+'$/^0$';b['selector']='^'+name+'$/^6$'
 cell('owners/'+name,'guest','^'+name+'$',a,b,'test-cycle')
for name in ['TestR9OwnershipStateH6FailedInitialization','TestR9OwnershipStateH6CallbackQuiescence','TestR9H2ReceiptConvertedProducerCycle','TestR9H2HostImportBulk']:
 cell('shape/'+name,'guest','^'+name+'$',side('shape-before'),side('shape-after'),'test-cycle')
for n in [1,8]:
 for size in [1<<20,8<<20]:cell(f'tinygo-idle/{n}/{size}','runtime','^TestFinalMeasurementIdleStacks$',side(FM_ENGINES='1',FM_STACK_BYTES=str(size)),side(FM_ENGINES=str(n),FM_STACK_BYTES=str(size)),'test-cycle','tinygo-0.42-patched-linux-amd64-threads-conservative')
for name in ['TestTinyGoForeignStackConcurrentGC','TestTinyGoForeignStackGCHostReentry','TestTinyGoForeignStackGCPreparedEntry','TestTinyGoForeignStackGCCloseThunkFailure']:
 cell('tinygo-active/'+name,'runtime','^'+name+'$',side(),side(),'test-cycle','tinygo-0.42-patched-linux-amd64-threads-conservative')
for shift in [6,12]:
 cell('tinygo-evacuation/'+str(shift),'native','^TestFinalMeasurementEvacuation$',side(FM_WORKERS='1',FM_SHIFT=str(shift),FM_EVAC='Off'),side(FM_WORKERS='1',FM_SHIFT=str(shift),FM_EVAC='On'),'test-cycle','tinygo-0.42-patched-linux-amd64-threads-conservative')
for on in ['Off','On']:
 cell('tinygo-evacuation-control/'+on,'native','^TestFinalMeasurementEvacuation$',side(FM_WORKERS='1',FM_SHIFT='12',FM_EVAC=on),side(FM_WORKERS='1',FM_SHIFT='12',FM_EVAC=on),'test-cycle','tinygo-0.42-patched-linux-amd64-threads-conservative')
cat=json.loads(blob('corpus/catalog.json')); census=[]
for e in cat['benchmarks']:
 p=R/'corpus'/e['artifact'];actual=sha(p.read_bytes()) if p.is_file() else None
 census.append(dict(id=e['id'],expected=e['artifact_sha256'],actual=actual,match=actual==e['artifact_sha256'],runtime='NOT_RUN',compile='NOT_RUN',gc_graph=False))
 for kind,sel in [('compile','^BenchmarkCompile$'),('runtime','^(BenchmarkExec|BenchmarkCommandExec)$')]:
  cell('corpus/'+kind+'/'+e['id'],'bench',sel,side('baseline'),side(),kind)
  rows[-1]['catalog_id']=e['id']
plan=dict(version=1,pins={'baseline':BASE,'port':SOURCE,'evidence':'2c9f72744dd6f414fc8f236c2d45aa39ce6ced69','shape-before':'8dcaa7095169a36eff93652a2b74b10a51900655','shape-after':'6e706159a0eb71b939bc0e9c9e0e32d5e03d5f4f'},cells=rows,blocks=6,order=['ABBA','BAAB'],benchtime='500ms',test_cycle_count=1,attempts=3,admission_c=60,discard_c=85,control_range=[.9,1.1],seed=20261102,status='PREPARATION_ONLY',catalog_sha256=sha(blob('corpus/catalog.json')))
(H/'plan.json').write_text(json.dumps(plan,indent=2)+'\n');(H/'census.json').write_text(json.dumps(census,indent=2)+'\n')
files=subprocess.check_output(['rg','-l','READY TO MEASURE','docs','--glob','*.md'],cwd=R,text=True).splitlines()
files=[p for p in files if '/final-measurement/' not in p]
files+=['docs/rfwgc-evidence/final-combined-5345/README.md']
(H/'inputs.json').write_text(json.dumps({p:sha((R/p).read_bytes()) for p in files},indent=2)+'\n')
print(json.dumps({'cells':len(rows),'accepted_samples':len(rows)*24,'max_attempt_samples':len(rows)*72,'catalog_ids':len(census)}))
