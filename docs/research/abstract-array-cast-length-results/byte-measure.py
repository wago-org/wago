import hashlib,json,os,pathlib,re,statistics,subprocess,time
root=pathlib.Path('/home/dev/Projects/wago-gc-abstract-array-length-pr')
data=pathlib.Path(__file__).resolve().parent
base='d7b8ac6ef1daf28a0bdd16747ec9ab1fbfba9e21'
rel='src/core/compiler/backend/railshot/amd64/gc_abstract_array_len.go'
def sha(p):return hashlib.sha256(p.read_bytes()).hexdigest()
def call(label,args,cwd=root,env=None):
    log=data/(label+'.log');meta=data/(label+'.json')
    if log.exists() or meta.exists():raise RuntimeError('Refusing to overwrite '+label)
    start=time.monotonic();p=subprocess.run(args,cwd=cwd,env=env,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT)
    log.write_text(p.stdout)
    row={'command':args,'cwd':str(cwd),'seconds':time.monotonic()-start,'returncode':p.returncode}
    meta.write_text(json.dumps(row,indent=2)+'\n');print(json.dumps(row),flush=True)
    if p.returncode:raise RuntimeError('Failed '+label+': see '+str(log))
    return row,p.stdout
head=subprocess.check_output(['git','rev-parse','HEAD'],cwd=root,text=True).strip()
if head!=base:raise RuntimeError('Worktree HEAD changed: '+head)
status=subprocess.check_output(['git','status','--short'],cwd=root,text=True)
(data/'source-status.txt').write_text(status)
(data/'production.patch').write_bytes(subprocess.check_output(['git','diff','--',rel],cwd=root))
control=data/'baseline.go';candidate=data/'candidate.go'
control.write_bytes(subprocess.check_output(['git','show',base+':'+rel],cwd=root));candidate.write_bytes((root/rel).read_bytes())
identity={'base':base,'file':rel,'baseline_source_sha256':sha(control),'candidate_source_sha256':sha(candidate),'GOMAXPROCS':1,'cpu':15,'samples_per_process':3,'benchtime':'200ms','order':['baseline','candidate','candidate','baseline'],'benchmark_unit':'one Invoke of 8192 guest-loop iterations; includes host call','runtime_test_sha256':sha(root/'src/wago/gc_abstract_array_cast_len_amd64_test.go')}
env=dict(os.environ,GOMAXPROCS='2',CGO_ENABLED='0',GOWORK='off',GOFLAGS='')
for label,source in [('baseline',control),('candidate',candidate)]:
    overlay=data/(label+'-overlay.json');overlay.write_text(json.dumps({'Replace':{str(root/rel):str(source)}},indent=2)+'\n')
    binary=data/(label+'.test')
    call('build-'+label,['go','test','-c','-overlay='+str(overlay),'-o',str(binary),'./src/wago'],env=env)
    identity[label+'_binary_sha256']=sha(binary)
    call('safety-'+label,[str(binary),'-test.run=^TestGCAbstractArrayCastLen','-test.count=1','-test.v'],cwd=root/'src/wago',env=env)
(data/'identity.json').write_text(json.dumps(identity,indent=2)+'\n')
print('Build and matched runtime checks complete; starting ABBA.',flush=True)
env=dict(env,GOMAXPROCS='1')
pattern=re.compile(r'^BenchmarkGCAbstractArrayCastLen/(\S+?)(?:-\d+)?\s+(\d+)\s+([\d.]+) ns/op\s+(\d+) B/op\s+(\d+) allocs/op$',re.M)
rows=[]
for i,label in enumerate(identity['order']):
    binary=data/(label+'.test')
    row,out=call('micro-'+str(i)+'-'+label,['taskset','-c','15',str(binary),'-test.run=^$','-test.bench=^BenchmarkGCAbstractArrayCastLen$','-test.benchmem','-test.count=3','-test.benchtime=200ms'],cwd=root/'src/wago',env=env)
    samples=[{'case':m[0],'invocations':int(m[1]),'ns_invoke':float(m[2]),'bytes_invoke':int(m[3]),'allocs_invoke':int(m[4])} for m in pattern.findall(out)]
    row.update(label=label,samples=samples);rows.append(row)
    (data/'samples.json').write_text(json.dumps(rows,indent=2)+'\n')
    if len(samples)!=6 or {s['case'] for s in samples}!={'loop','allocate-across-length'}:raise RuntimeError('Missing expected benchmark cases/samples')
summary={}
for case in ['loop','allocate-across-length']:
    vals={label:[s['ns_invoke'] for row in rows if row['label']==label for s in row['samples'] if s['case']==case] for label in ['baseline','candidate']}
    med={label:statistics.median(v) for label,v in vals.items()}
    summary[case]={'samples_ns_invoke':vals,'median_ns_invoke':med,'range_ns_invoke':{label:[min(v),max(v)] for label,v in vals.items()},'candidate_reduction_percent':100*(1-med['candidate']/med['baseline'])}
if sha(root/rel)!=identity['candidate_source_sha256']:raise RuntimeError('Worktree emitter changed during measurement')
(data/'summary.json').write_text(json.dumps(summary,indent=2)+'\n')
print(json.dumps(summary,indent=2),flush=True)
