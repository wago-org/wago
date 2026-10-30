"""Reuse Stage2 quiet-window method. Each cell runs all validated adapters serially."""
import os,pathlib,time,json,subprocess,datetime,types,fcntl
OUT=pathlib.Path(__file__).resolve().parent
source=(OUT.parent/'stage2-benchmark/measure.py').read_text()
# Reuse the exact /proc CPU and process accounting helpers, without script entrypoint.
ns={'os':os,'pathlib':pathlib,'time':time,'datetime':datetime,'subprocess':subprocess,'json':json}
exec(source[source.index('def processes():'):source.index('last_progress=time.monotonic()')],ns)
def thermal():
 values=[]
 for p in pathlib.Path('/sys/class/hwmon').glob('hwmon*/temp*_input'):
  if (p.parent/'name').read_text().strip() not in ('k10temp','coretemp','cpu_thermal'): continue
  try: values.append({'path':str(p),'c':int(p.read_text())/1000})
  except (OSError,ValueError): pass
 return {'available':bool(values),'sensors':values,'admit':all(x['c']<=60 for x in values)}
def append(file,row):
 with (OUT/file).open('a') as f:f.write(json.dumps(row)+'\n')
def main(tmp,env,manifest):
 # Complete the primary analysis-on baseline oracle matrix before taking timings.
 for tag in ['', 'wagodebug','wago_regalloccheck']:
  suffix='-'+tag if tag else ''
  for m in manifest:
   path=OUT/('validate-baseline-'+(tag or 'ordinary')+'-'+m['id']+'.txt')
   if path.exists():path.rename(path.with_name(path.name.replace('validate-baseline-', 'validate-baselineOff-')))
   cmd=[str(tmp/('baseline'+suffix+'.test')),'-test.run=^$','-test.bench=^(BenchmarkExec|BenchmarkCommandExec)$','-test.benchtime=1x','-test.timeout=2m','-wago.corpus='+m['id']]
   with path.open('w') as f:p=subprocess.run(cmd,cwd=tmp/'baseline/bench/suite',env=env|{'STAGE2_ON':'1'},stdout=f,stderr=subprocess.STDOUT)
   print('validate baselineOn',tag or 'ordinary',m['id'],p.returncode,flush=True)
 metadata={'baseline':'6ceb862219e64480c0503836b19ee1e02b209cc7','stage2':'9d7e4fdf7925265ee0c7db9246e66f0707ee2d76','primary_options':{'baseline':'analysis-on','stage2':'consumer-on'},'go':subprocess.check_output(['/home/dev/.local/share/mise/installs/go/1.27.1/bin/go','version'],env=env,text=True).strip(),'environment':{k:env.get(k) for k in ['GOROOT','GOMAXPROCS','GOFLAGS','GOEXPERIMENT','GOGC','GOMEMLIMIT']},'binaries':{},'harness':{}}
 import hashlib
 for p in tmp.glob('*.test'):metadata['binaries'][p.name]=hashlib.sha256(p.read_bytes()).hexdigest()
 for rev in ['baseline','stage2']:
  for rel in ['bench/suite/suite_test.go','bench/suite/command_corpus_test.go','bench/suite/stage2_corpus_test.go','bench/internal/semanticcorpus/run.go']:
   metadata['harness'][rev+'/'+rel]=hashlib.sha256((tmp/rev/rel).read_bytes()).hexdigest()
 (OUT/'host.json').write_text(json.dumps(metadata,indent=2)+'\n')
 subprocess.run([str(tmp/'stats.test'),'-test.run=^TestStage2Inventory$','-test.v','-wago.corpus=all'],cwd=tmp/'stage2/bench/suite',env=env,stdout=open(OUT/'inventory.txt','w'),stderr=subprocess.STDOUT,check=True)
 # No existing machine lease service/tool or live lease discovered. Canonical main-checkout flock.
 lease=pathlib.Path('/home/dev/Projects/wago/.performance-measurement.lock')
 with lease.open('a+') as f:
  fcntl.flock(f,fcntl.LOCK_EX|fcntl.LOCK_NB)
  append('lease.jsonl',{'event':'acquired','pid':os.getpid(),'path':str(lease),'authorization':'owner explicitly authorizes corpus measurements','utc':datetime.datetime.now(datetime.timezone.utc).isoformat()})
  allowed=sorted(os.sched_getaffinity(0));ns['a']=types.SimpleNamespace(cpu=allowed[0])
  snap=ns['snapshot'];obs=ns['observation']
  before=snap();time.sleep(2);after=snap()
  choices=[]
  for cpu in allowed:
   ns['a'].cpu=cpu;r=obs(before,after);choices.append((max([r['external_host_pct']]+list(r['watched_cpu_busy_pct'].values())),cpu,r))
  _,cpu,_=min(choices);ns['a'].cpu=cpu
  (OUT/'cpu-selection.json').write_text(json.dumps({'selected':cpu,'observed':choices,'thermal':thermal()},indent=2)+'\n')
  # Keep all entries in inventory; missing/unexecutable/failed adapters are explicit runtime gaps.
  eligible=[]
  for m in manifest:
   paths=[OUT/('validate-'+rev+'-ordinary-'+m['id']+'.txt') for rev in ['baseline','stage2']]
   if all('ns/op' in p.read_text() and 'FAIL' not in p.read_text() for p in paths):eligible.append(m['id'])
  (OUT/'timing-eligible.json').write_text(json.dumps(eligible,indent=2)+'\n')
  last=time.monotonic()
  try:
   for m in manifest:
    if m['id'] not in eligible:continue
    for repeat in range(6):
     for name in (['baseline','stage2'] if repeat%2==0 else ['stage2','baseline']):
      label=f"{m['id']}-{repeat+1:02d}-{name}";attempt=0
      while True:
       stable=0
       while stable<3:
        if time.monotonic()-last>300:raise RuntimeError('No admitted measurement progress for five minutes; threshold unchanged')
        before=snap();time.sleep(2);r=obs(before,snap());r.update(label=label,thermal=thermal());append('admission.jsonl',r)
        stable=stable+1 if not r['noisy'] else 0
       attempt+=1
       cmd=['taskset','-c',str(cpu),str(tmp/(name+'.test')),'-test.run=^$','-test.bench=^(BenchmarkExec|BenchmarkCommandExec)$','-test.benchtime=600ms','-test.timeout=3m','-wago.corpus='+m['id']]
       proc=subprocess.Popen(cmd,cwd=tmp/name/'bench/suite',env=env|{'STAGE2_ON':'1'},stdout=subprocess.PIPE,stderr=subprocess.STDOUT)
       rows=[];before=snap()
       while True:
        try:output,_=proc.communicate(timeout=.25);done=True
        except subprocess.TimeoutExpired:done=False
        after=snap()
        if after[2]-before[2]>=.25:rows.append(obs(before,after,proc.pid))
        before=after
        if done:break
       good=proc.returncode==0 and bool(rows) and not any(r['noisy'] for r in rows)
       append('during-timing.jsonl',{'cell':label,'attempt':attempt,'accepted':good,'returncode':proc.returncode,'observations':rows,'thermal_end':thermal()})
       (OUT/(('' if good else 'rejected-')+label+f'-attempt{attempt}.txt')).write_bytes(output)
       print('timing',label,attempt,'accepted' if good else 'rejected',flush=True)
       if good:last=time.monotonic();break
       if time.monotonic()-last>300:raise RuntimeError('No accepted measurement progress for five minutes; reassess required')
  except RuntimeError as e:
   (OUT/'timing-blocker.txt').write_text(str(e)+'\n')
  finally:append('lease.jsonl',{'event':'released','pid':os.getpid(),'utc':datetime.datetime.now(datetime.timezone.utc).isoformat()})
