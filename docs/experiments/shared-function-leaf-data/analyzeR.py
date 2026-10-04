from pathlib import Path
import json,re,csv,statistics as st,math,random,subprocess,hashlib
p=Path(__file__).resolve().parent;o=p/'final-analysis';o.mkdir(exist_ok=True)
runs=json.loads((p/'final-measurement-runs.json').read_text())
assert len(runs)==126 and all(r['exit']==0 for r in runs), 'measurements incomplete'
def write(name,rows):
 keys=list(dict.fromkeys(k for r in rows for k in r))
 with(o/name).open('w',newline='')as f:
  w=csv.DictWriter(f,fieldnames=keys,lineterminator='\n');w.writeheader();w.writerows(rows)
def jsons(text):
 for l in text.splitlines():
  if '{'not in l:continue
  try:d=json.loads(l[l.index('{'):])
  except ValueError:continue
  yield d
timing=[];mem=[];resources=[];aggregates=[];files={};signatures={}
synthetic={'small','pressure','join','large','deep','locals','many','large_then_small'}
for run in runs:
 name=run['kind'];rev=run['revision'];txt=(p/(run['kind']and f"final-{name}-block{run['block']}-{run['sequence']}-{rev}.txt")).read_text();ident=dict(revision=rev,cohort=name,process=f"final-{name}-block{run['block']}-{run['sequence']}-{rev}",block=run['block'])
 res=json.loads((p/(ident['process']+'.resource.json')).read_text());resources.append(dict(ident,**res))
 if name in ['timing','long','workers']:
  files.setdefault((rev,name),[]).append(txt)
  rows=[]
  for l in txt.splitlines():
   if not l.startswith('Benchmark'):continue
   parts=l.split();key=re.sub(r'-[0-9]+$','',parts[0]);row=dict(ident,benchmark=key,iterations=int(parts[1]));row.update({parts[i+1]:float(parts[i])for i in range(2,len(parts)-1,2)});rows.append(row)
  assert rows and len({r['benchmark']for r in rows})==len(rows)
  sig={r['benchmark']for r in rows}
  assert sig==signatures.setdefault(name,sig)
  timing.extend(rows)
  if name=='timing':
   groups={}
   for r in rows:
    bn=r['benchmark']
    for group,yes in [('AllNative',bn.startswith(('BenchmarkSharingNative/','BenchmarkCompile/'))),('MigratedNative',bn.startswith('BenchmarkSharingNative/')and bn.split('/')[1]in synthetic),('AllFull',bn.startswith(('BenchmarkSharingFull/','BenchmarkCompileFull/'))),('AllExec',bn.startswith(('BenchmarkSharingExec/','BenchmarkExec/'))),('MigratedExec',bn.startswith('BenchmarkSharingExec/')and bn.split('/')[1]in synthetic)]:
     if yes:groups.setdefault(group,[]).append(r['ns/op'])
   for group,vs in groups.items():aggregates.append(dict(ident,benchmark='BenchmarkAggregate/'+group,**{'ns/op':math.exp(st.mean(math.log(x)for x in vs))}))
 else:
  scope='runtime'if name.startswith('runtime')else None;prev={}
  for line in txt.splitlines():
   if line.startswith('=== RUN'):scope='public'if line.split()[-1]=='TestSharingMemory'else'backend'
   for d in jsons(line):
    if 'phase'not in d:continue
    assert scope
    assert d['modules']==(36 if d['phase'].startswith('retained_')else 0)
    row=dict(ident,scope=scope,**d);n=row.pop('native',{});row.update({'native_'+k:v for k,v in n.items()});row.pop('statm',None)
    if scope in prev:row.update(allocation_bytes_delta=d['total_alloc']-prev[scope]['total_alloc'],allocation_count_delta=d['mallocs']-prev[scope]['mallocs'])
    prev[scope]=d;mem.append(row)
  assert prev and all(d['phase']==('released_2'if s=='backend'else'runtime_released')for s,d in prev.items())
for rev in ['A','C','R']:
 a=[r for r in aggregates if r['revision']==rev]
 files[(rev,'aggregates')]=['\n'.join(f"{r['benchmark']} 1 {r['ns/op']:.9f} ns/op"for r in a)]
for(rev,kind),texts in files.items():(o/(rev+'-'+kind+'.txt')).write_text('\n'.join(texts))
for pair in [('A','R'),('C','R'),('A','C')]:
 for kind in ['timing','long','workers','aggregates']:
  for fmt in ['text','csv']:
   r=subprocess.run(['benchstat','-format='+fmt,*[str(o/(rev+'-'+kind+'.txt'))for rev in pair]],capture_output=True,text=True,check=True);(o/('-'.join(pair)+'-'+kind+'.'+fmt)).write_text(r.stdout+r.stderr if fmt=='text'else r.stdout)
def summarize(rows,ids,metrics,out):
 groups={}
 for r in rows:
  for m in metrics(r):
   if isinstance(r.get(m),(int,float)):groups.setdefault(tuple(r[x]for x in ids)+(m,),{}).setdefault(r['revision'],[]).append(r[m])
 result=[];rng=random.Random(802)
 for k,g in sorted(groups.items()):
  for a,b in [('A','R'),('C','R'),('A','C')]:
   if a not in g or b not in g:continue
   av,bv=g[a],g[b];am,bm=st.median(av),st.median(bv);lo=hi=None
   if out=='timing-summary.csv'and am>0 and all(x>0 for x in av):
    draws=sorted(100*(st.median(rng.choices(bv,k=len(bv)))/st.median(rng.choices(av,k=len(av)))-1)for _ in range(3000));lo=draws[74];hi=draws[2924]
   result.append(dict(zip(ids,k[:-1]),comparison=a+'/'+b,metric=k[-1],before=am,after=bm,absolute_delta=bm-am,percent_delta=100*(bm/am-1)if am else None,before_n=len(av),after_n=len(bv),before_min=min(av),before_max=max(av),after_min=min(bv),after_max=max(bv),bootstrap_95_low=lo,bootstrap_95_high=hi))
 write(out,result)
write('raw-timing.csv',timing);write('aggregates.csv',aggregates);write('raw-memory.csv',mem);write('process-resources.csv',resources)
summarize(timing+aggregates,['cohort','benchmark'],lambda r:[m for m in ['ns/op','B/op','allocs/op']if m in r],'timing-summary.csv')
summarize(mem,['cohort','scope','phase'],lambda r:[k for k in r if k not in ['revision','cohort','scope','phase','process','block']],'memory-summary.csv')
summarize([dict(r,phase='process',scope='combined'if r['cohort'].startswith('memory')else'runtime')for r in resources if r['cohort']not in ['timing','long','workers']],['cohort','scope','phase'],lambda r:[k for k in r if k.startswith('peak')],'process-summary.csv')
diags=[]
for rev,arch,filename in [(r,'amd64',r+'-native-diagnostic.log')for r in ['A','C','R']]+[('R','arm64','R-arm64-suite-final.log')]:
 for d in jsons((p/filename).read_text()):
  if 'stats'not in d:continue
  stats=d.pop('stats');fs=[x for x in(stats.get('Funcs')or[])if x]
  row=dict(revision=rev,arch=arch,**d,migrated_functions=sum(bool(x.get('SharedScalar'))for x in fs),migrated_body_bytes=sum(x.get('ScalarBodyBytes',0)for x in fs if x.get('SharedScalar')),frame_bytes=sum(x.get('FrameBytes',0)for x in fs),spill_slots=sum(x.get('MaxSpillSlots',0)for x in fs),scalar_spills=sum(x.get('ScalarSpills',0)for x in fs),scalar_reloads=sum(x.get('ScalarReloads',0)for x in fs),admission_ns=sum(x.get('ScalarAdmissionNanos',0)for x in fs))
  row.update({'compile_'+k:json.dumps(v)if isinstance(v,(dict,list))else v for k,v in stats.get('Compile',{}).items()});row.update({'native_'+k:v for k,v in stats.get('NativeSize',{}).items()});diags.append(row)
write('diagnostics.csv',diags)
for rev in ['A','C','R']:
 sig={d['name']:d['corpus_sha256']for d in diags if d['revision']==rev and d['arch']=='amd64'}
 assert sig=={d['name']:d['corpus_sha256']for d in diags if d['revision']=='R'and d['arch']=='arm64'}
for arch in ['amd64','arm64']:
 ds=[d for d in diags if d['revision']=='R'and d['arch']==arch];print('coverage',arch,sum(d['migrated_functions']for d in ds),sum(d['functions']for d in ds),sum(d['migrated_body_bytes']for d in ds),sum(d['body_bytes']for d in ds))
print('ANALYZED',len(timing),len(mem),len(resources))
