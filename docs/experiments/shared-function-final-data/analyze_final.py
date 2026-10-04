from pathlib import Path
import json,re,statistics,math,subprocess,csv
p=Path(__file__).parent;q=p/'results';q.mkdir(exist_ok=True);revs=['M','B','R','P']
def summary(v):return dict(median=statistics.median(v),min=min(v),max=max(v),n=len(v))
raw=[]
for kind in ['timing','long','workers']:
 rows={}
 for rev in revs:
  files=sorted(p.glob(f'final-{kind}-block*-{rev}.txt'));assert len(files)==6,(kind,rev,len(files))
  (q/(rev+'-'+kind+'.txt')).write_text('\n'.join(f.read_text()for f in files));rows[rev]=[]
  for f in files:
   sample={}
   for l in f.read_text().splitlines():
    if not l.startswith('Benchmark'):continue
    tok=l.split();key=tok[0][9:];sample[key]={tok[i+1]:float(tok[i])for i in range(2,len(tok)-1,2)}
    raw.append(dict(kind=kind,revision=rev,file=f.name,benchmark=key,**sample[key]))
   rows[rev].append(sample)
 if kind=='timing':
  assert all(len(x)==47 for v in rows.values()for x in v)
  names=['small','pressure','join','large','deep','locals','many','large_then_small'];groups={}
  for stage in ['Native','Full','Exec']:groups['Migrated'+stage]=['Sharing'+stage+'/'+x for x in names]
  for stage,bench in [('Native','Compile'),('Full','CompileFull'),('Exec','Exec')]:existing=['tiny.add','fib_rec.fib','many_funcs.run','json-as.serializeN','json-as.deserializeN','xxhash.xxhash_run']if stage=='Exec'else['tiny','fib_rec','many_funcs','xxhash','json-as'];groups['Existing'+stage]=[bench+'/'+x for x in existing];groups['All'+stage]=['Sharing'+stage+'/'+x for x in names+['fallback']]+groups['Existing'+stage]
  for rev in revs:
   out=['goos: linux','goarch: amd64','pkg: derived-geomeans']
   for sample in rows[rev]:
    for name,keys in groups.items():out.append('Benchmark'+name+'\t1\t'+str(math.exp(sum(math.log(sample[k]['ns/op'])for k in keys)/len(keys)))+' ns/op')
   (q/(rev+'-aggregates.txt')).write_text('\n'.join(out)+'\n')
  summary_t={name:{rev:{metric:summary([s[name][metric]for s in rows[rev]])for metric in rows[rev][0][name]}for rev in revs}for name in rows['M'][0]}
  (q/'timing-summary.json').write_text(json.dumps(summary_t,indent=2))
 for pair in ['MB','MR','MP','BP','RP']:
  for fmt in ['text','csv']:
   with(q/(pair+'-'+kind+'.'+fmt)).open('w')as out, (q/(pair+'-'+kind+'-'+fmt+'-notes.txt')).open('w')as err:subprocess.run(['benchstat','-format='+fmt,str(q/(pair[0]+'-'+kind+'.txt')),str(q/(pair[1]+'-'+kind+'.txt'))],stdout=out,stderr=err,check=True)
for pair in ['MB','MR','MP','BP','RP']:
 for fmt in ['text','csv']:
  with(q/(pair+'-aggregates.'+fmt)).open('w')as out:subprocess.run(['benchstat','-format='+fmt,str(q/(pair[0]+'-aggregates.txt')),str(q/(pair[1]+'-aggregates.txt'))],stdout=out,check=True)
(p/'results/raw-timing.json').write_text(json.dumps(raw,indent=2))
mem=[]
for kind in ['memory','memory-auto','runtime','runtime-auto']:
 for rev in revs:
  files=sorted(p.glob(f'final-{kind}-block*-{rev}.txt'));assert len(files)==6
  for f in files:
   scenario='RuntimeMemory'if kind.startswith('runtime')else''
   for l in f.read_text().splitlines():
    if l.startswith('=== RUN'):scenario=l.split()[-1]
    if l.startswith('{')or': {'in l:
     d=json.loads(l[l.index('{'):]);d.update(revision=rev,kind=kind,file=f.name,scenario=scenario);mem.append(d)
(q/'raw-memory.json').write_text(json.dumps(mem,indent=2));ms={}
for kind in ['memory','memory-auto','runtime','runtime-auto']:
 for scenario in sorted(set(x['scenario']for x in mem if x['kind']==kind)):
  k=kind+'/'+scenario;ms[k]={}
  selected=[x for x in mem if x['kind']==kind and x['scenario']==scenario]
  for phase in dict.fromkeys(x['phase']for x in selected):
   ms[k][phase]={}
   for metric in ['heap_alloc','heap_inuse','heap_objects','heap_released','rss_bytes','peak_rss_kib','code_bytes','mapped_bytes','payload_bytes_page_rounded']:
    vs={r:[x[metric]for x in selected if x['revision']==r and x['phase']==phase and metric in x]for r in revs}
    if not vs['M']:continue
    ms[k][phase][metric]={r:summary(v)for r,v in vs.items()}
  ms[k]['compile_release_delta']={}
  for rev in revs:
   ds=[];ns=[]
   for filename in sorted(set(x['file']for x in selected if x['revision']==rev)):
    phases={x['phase']:x for x in selected if x['file']==filename};a=phases['initial'];b=phases['compile_release_270'];ds.append(b['total_alloc']-a['total_alloc']);ns.append(b['mallocs']-a['mallocs'])
   ms[k]['compile_release_delta'][rev]={'bytes':summary(ds),'allocs':summary(ns)}
(q/'memory-summary.json').write_text(json.dumps(ms,indent=2))
process=[dict(file=f.stem,**json.loads(f.read_text()))for f in sorted(p.glob('final-*.resource.json'))];(q/'process-resources.json').write_text(json.dumps(process,indent=2))
# Compact all diagnostic records without megabytes of per-function encoding fields.
diag=[]
for arch in ['amd64','arm64']:
 for rev in ['M','B','R','Z','T','P']:
  f=p/(rev+('-stage-diagnostic.txt'if arch=='amd64'else'-arm64-diagnostic.txt'))
  if not f.exists():continue
  for l in f.read_text().splitlines():
   if ': {'not in l:continue
   d=json.loads(l[l.index('{'):]);fs=d['stats']['Funcs'];m=[x for x in fs if x.get('SharedScalar')]
   diag.append(dict(architecture=arch,revision=rev,name=d['name'],functions=d['functions'],body_bytes=d['body_bytes'],corpus_sha256=d['corpus_sha256'],native_sha256=d['native_sha256'],code_bytes=d['code_bytes'],migrated_functions=len(m),migrated_body_bytes=sum(x.get('ScalarBodyBytes',0)for x in m),frame_bytes=sum(x['FrameBytes']for x in fs),spill_slots=sum(x['MaxSpillSlots']for x in fs),scalar_spills=sum(x.get('ScalarSpills',0)for x in fs),scalar_reloads=sum(x.get('ScalarReloads',0)for x in fs),admission_ns=sum(x.get('ScalarAdmissionNanos',0)for x in fs),compile_resources=d['stats']['Compile'],native_storage=d['stats']['NativeSize']))
(q/'diagnostic-summary.json').write_text(json.dumps(diag,indent=2))
print('analysis complete')
