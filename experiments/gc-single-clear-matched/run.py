import pathlib,datetime,time,os,json,subprocess,hashlib,argparse
p=argparse.ArgumentParser();p.add_argument("--artifact-dir",type=pathlib.Path,required=True);args=p.parse_args()
O=args.artifact_dir.resolve();cpu=min(os.sched_getaffinity(0));rows=[]
prov={'source_control':'35faf6c9','source_candidate':'fadea926','binary_sha256':{p.name:hashlib.sha256(p.read_bytes()).hexdigest() for p in O.glob('*.test')},'selected_cpu':cpu,'allowed_affinity':sorted(os.sched_getaffinity(0)),'cpuinfo':pathlib.Path('/proc/cpuinfo').read_text(),'protocol':'same-workload within each core mode, 5 alternating processes, direct500ms/graph, compact128epochs; no V8; ownjobs-paused not exclusivehost'}
def utc():return datetime.datetime.now(datetime.timezone.utc).isoformat()
def run(stage,rnd,kind,label,case,extra,env):
 cmd=['taskset','-c',str(cpu),str(O/f'{kind}-{label}.test'),'-test.run=^$',*extra,'-test.benchmem','-test.count=1'];t=time.monotonic();start=utc();load=os.getloadavg()
 p=subprocess.run(cmd,env=dict(os.environ,GOMAXPROCS='1',**env),text=True,capture_output=True)
 r={'stage':stage,'round':rnd,'kind':kind,'variant':label,'case':case,'started_utc':start,'ended_utc':utc(),'load_before':load,'load_after':os.getloadavg(),'wall_seconds':time.monotonic()-t,'returncode':p.returncode,'stdout':p.stdout,'stderr':p.stderr};rows.append(r);(O/'results.json').write_text(json.dumps({'provenance':prov,'rows':rows},indent=2));print(stage,rnd,kind,label,case,p.returncode,flush=True)
 if p.returncode or 'PASS' not in p.stdout:raise SystemExit('Failure retained')
# First graph/counter audits, excluded from timing summaries.
policies=['promote','age2','eden'];shapes=['dead-heavy','persistent344KiB','array-bulk','rolling84x4096','dense-persistent','old-young-mutation']
for label in ['control','candidate']:
 run('audit',0,'direct',label,'all',['-test.bench=^(BenchmarkMarkCleanupGraphs|BenchmarkFullTracingShapes)$','-test.benchtime=1x'],{})
 for p in policies:
  for s in shapes:run('audit',0,'compact',label,p+'/'+s,[f'-test.bench=^BenchmarkEdenTraffic$/^{p}$/^{s}$','-test.benchtime=1x'],{'GC_CORE_POLICY':p})
for rnd in range(5):
 order=['control','candidate'] if rnd%2==0 else ['candidate','control']
 for label in order:run('timed',rnd,'direct',label,'all',['-test.bench=^(BenchmarkMarkCleanupGraphs|BenchmarkFullTracingShapes)$','-test.benchtime=500ms'],{})
 for i in range(len(shapes)):
  s=shapes[(i+rnd)%len(shapes)]
  for j in range(len(policies)):
   p=policies[(j+rnd)%len(policies)]
   for label in order:run('timed',rnd,'compact',label,p+'/'+s,[f'-test.bench=^BenchmarkEdenTraffic$/^{p}$/^{s}$','-test.benchtime=128x'],{'GC_CORE_POLICY':p})
