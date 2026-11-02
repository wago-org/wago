#!/usr/bin/env python3
"""Read-only source and archive audit; no build or runtime execution."""
import collections,datetime,hashlib,json,re,subprocess,zipfile
from pathlib import Path
from verify import HERE,ROOT,SOURCE,DRIVER,REV,BASE,blob,sha

def save(n,x): (HERE/n).write_text(json.dumps(x,indent=2)+'\n')
def command(*args,cwd=ROOT): return subprocess.check_output(args,cwd=cwd).decode()
old=HERE.parent/'native-run-41447853354'
previous='c25943944'
assert (old/'README.md').read_bytes()==blob(previous,str((old/'README.md').relative_to(ROOT)))
source_paths=['src/wago/gc_native_reservation_arm64_test.go','src/core/runtime/gc/native/native_view.go','src/wago/gc_extern_multi_table_test.go','src/wago/gc_conversion_store_host.go','src/core/compiler/backend/railshot/arm64/callexec_arm64_test.go','src/core/runtime/arm64spike/spike.go','src/core/runtime/arm64spike/spike_arm64.s']
source_audit={p:{'sha256':sha(blob(SOURCE,p)),'text':blob(SOURCE,p).decode()} for p in source_paths}
save('pinned-source-audit.json',{'files':source_audit,'source_commit':command('git','show','--format=fuller','--stat',SOURCE),'source_parent_diff':command('git','diff',SOURCE+'^',SOURCE,'--','src','tests','.github'),'delta_from_prior_source':command('git','diff','7e41fafbbcd737ed1edce11b94b43b4eda134dcf',SOURCE,'--','src','tests','.github'),'review':blob('57c04aa09c53321e86a8358ae7fc53688af82b2b','docs/rfwgc-evidence/native-current-pin-recheck/README.md').decode(),'prior_audit_commit':previous})
failures=[];closure=[];unfinished=[];prior_comparison=[]
for mode in ['wagodebug','wago_regalloccheck']:
 z=zipfile.ZipFile(HERE/'artifacts'/f'darwin-ownership-{mode}-1.zip');pz=zipfile.ZipFile(old/'artifacts'/f'darwin-ownership-{mode}-1.zip');prefix=f'receipts-{mode}/'
 for row in json.loads(z.read(prefix+'results.json')):
  lane=row['id'];member=prefix+lane+'/raw.jsonl'
  es=[json.loads(l) for l in z.read(member).splitlines() if l.startswith(b'{')]
  started=collections.Counter((e.get('Package'),e['Test']) for e in es if e.get('Test') and e['Action']=='run')
  ended=collections.Counter((e.get('Package'),e['Test']) for e in es if e.get('Test') and e['Action'] in ['pass','fail','skip'])
  unfinished.append({'mode':mode,'lane':lane,'started_without_terminal':[{'package':k[0],'test':k[1],'count':v} for k,v in (started-ended).items()]})
  for e in es:
   if 'fatal error:' in e.get('Output',''): failures.append({'archive':str(Path('artifacts')/f'darwin-ownership-{mode}-1.zip'),'member':member,'event':e})
   if e.get('Test')=='TestGCExternConversionAcrossReferenceTables' and e['Action'] in ['pass','fail','skip']:closure.append({'mode':mode,'lane':lane,'event':e})
  if lane.startswith(('race-','diagnostics-established-')):
   pe=[json.loads(l) for l in pz.read(member).splitlines() if l.startswith(b'{')]
   for test in ['TestGCARM64LiveStructDoesNotPrepareUnusedHandles','TestTwoIntegerResultRegisterCall']:
    prior_comparison.append({'mode':mode,'lane':lane,'test':test,'prior_events':[e for e in pe if e.get('Test')==test and e['Action'] in ['run','pass','fail','skip']],'current_events':[e for e in es if e.get('Test')==test and e['Action'] in ['run','pass','fail','skip']]})
 prefix=f'contracts-{mode}/'
 es=[json.loads(l) for l in z.read(prefix+'tests.jsonl').splitlines() if l.startswith(b'{')]
 closure.extend({'mode':mode,'lane':'focused','event':e} for e in es if e.get('Test')=='TestGCExternConversionAcrossReferenceTables' and e['Action'] in ['pass','fail','skip'])
assert len(closure)==12 and all(x['event']['Action']=='pass' for x in closure)
assert len(failures)==3
save('first-failures.json',sorted(failures,key=lambda f:f['event']['Time']))
save('closure-events.json',closure);save('unfinished-tests.json',unfinished);save('prior-event-comparison.json',prior_comparison)
# Read-only sibling snapshot, before attributing the unresolved native bridge failure.
siblings=[]
for root in ['/home/dev/Projects/slipstream','/home/dev/Projects/starshine-mb']:
 head=command('git','rev-parse','HEAD',cwd=root).strip()
 patterns='enterNativeSpike|spikeStackTop|StructAllocState|TestTwoIntegerResultRegisterCall|checkptr|foreign.stack'
 proc=subprocess.run(['git','grep','-n','-i','-E',patterns,head,'--','src'],cwd=root,capture_output=True,text=True)
 assert proc.returncode in [0,1]
 siblings.append({'root':root,'head':head,'search':patterns,'exit':proc.returncode,'matches':proc.stdout,'interpretation':'No matching Wago bridge/test implementation found; this does not certify sibling correctness.'})
save('sibling-audit.json',siblings)
run=json.loads((HERE/'run.json').read_text());jobs=json.loads((HERE/'jobs.json').read_text())['jobs'];arts=json.loads((HERE/'artifacts.json').read_text())['artifacts']
ordinary=next(j for j in jobs if j['name']=='Pinned Darwin correctness / ordinary')
assert ordinary['conclusion']=='cancelled' and ordinary['steps']==[] and ordinary['runner_id']==0
assert len(arts)==5 and not any(a['name']=='darwin-ownership-ordinary-1' for a in arts)
save('availability.json',{'requested_artifacts':6,'available_artifacts':5,'missing':'darwin-ownership-ordinary-1','ordinary':ordinary,'job_conclusions':dict(collections.Counter(j['conclusion'] for j in jobs)),'note':'Both connector and direct GitHub REST agreed. No ordinary execution is credited. No cancellation actor or cause is established by these records.'})
def dt(s):return datetime.datetime.fromisoformat(s.replace('Z','+00:00'))
save('cost.json',{'source':'GitHub historical timestamps only; not measurements','created_at':run['created_at'],'updated_at':run['updated_at'],'run_window_seconds':(dt(run['updated_at'])-dt(run['created_at'])).total_seconds(),'jobs':[{'name':j['name'],'conclusion':j['conclusion'],'started_at':j['started_at'],'completed_at':j['completed_at'],'wall_seconds':(dt(j['completed_at'])-dt(j['started_at'])).total_seconds()} for j in jobs if j['conclusion']!='skipped'],'ceiling_job_minutes':1173,'actual_billed_cost':'unknown','repair_and_revalidation_duration':'unknown'})
# Keep the complete archive and expanded original logs. Never edit failure text.
z=zipfile.ZipFile(HERE/'full-job-logs.zip');dest=HERE/'job-logs';dest.mkdir(exist_ok=True)
for n in z.namelist():
 assert not Path(n).is_absolute() and '..' not in Path(n).parts
 p=dest/n;p.parent.mkdir(parents=True,exist_ok=True);p.write_bytes(z.read(n))
print(json.dumps({'fatal_events':len(failures),'multi_table_passes':len(closure),'ordinary':'CANCELLED_NO_ARTIFACT','sibling_matches':[s['exit'] for s in siblings]}))
# Parse focused/baseline package censuses too; nonempty files alone are insufficient.
censuses=[]
for a in arts:
 z=zipfile.ZipFile(HERE/'artifacts'/(a['name']+'.zip'));target,_,mode,_=a['name'].split('-')
 for pre in ([f'contracts-{mode}/',f'baseline-{mode}/'] if target=='darwin' else ['']):
  raw=z.read(pre+'package-census.jsonl').decode();raw=raw[raw.index('{'):];decoder=json.JSONDecoder();records=[]
  while raw.strip():
   record,end=decoder.raw_decode(raw.lstrip());raw=raw.lstrip()[end:];records.append(record)
  assert records and not any(r.get('Error') or r.get('DepsErrors') for r in records)
  censuses.append({'artifact':a['name'],'prefix':pre,'packages':len(records),'all_package_errors_absent':True})
save('focused-package-census.json',censuses)
# Known credential patterns only; no claim to detect unknown formats.
hits=[]
patterns=[rb'gh[pousr]_[A-Za-z0-9]{30,}',rb'github_pat_[A-Za-z0-9_]{30,}',rb'-----BEGIN (?:RSA |OPENSSH |EC )?PRIVATE KEY-----']
for f in list((HERE/'artifacts').glob('*.zip'))+[HERE/'full-job-logs.zip']:
 z=zipfile.ZipFile(f)
 for n in z.namelist():
  b=z.read(n)
  if any(re.search(pattern,b) for pattern in patterns):hits.append({'archive':f.name,'member':n})
save('log-safety.json',{'known_secret_pattern_hits':hits,'limit':'Not proof against unknown secret formats.'})
assert not hits
