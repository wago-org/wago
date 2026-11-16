import subprocess,os,json,pathlib,time,datetime,hashlib,platform
p=pathlib.Path('/workspace/shared/gc-eden-core-evidence');policies=['promote','age2','eden','eden-same-fresh'];shapes=['dead-heavy','retain344KiB','array-bulk','dense-persistent','old-young-mutation'];rows=[]
def utc():return datetime.datetime.now(datetime.timezone.utc).isoformat()
prov={'source':'b893312c','harness_source':'6ad64d8b','binary_sha256':hashlib.sha256((p/'core.test').read_bytes()).hexdigest(),'platform':platform.platform(),'cpu':pathlib.Path('/proc/cpuinfo').read_text(),'affinity':sorted(os.sched_getaffinity(0)),'tags':['wago_gc_compact_eden','wago_gc_compact_age2'],'protocol':'20 audits1x then5 rotating rounds fixed8epochs, core-only same-source'}
for stage,rounds,epochs in [('audit',1,1),('timed',5,8)]:
 for rnd in range(rounds):
  for sj in range(5):
   shape=shapes[(sj+rnd)%5]
   for pj in range(4):
    policy=policies[(pj+rnd)%4];t=time.monotonic();start=utc();load=os.getloadavg()
    r=subprocess.run([str(p/'core.test'),'-test.run=^$',f'-test.bench=^BenchmarkEdenTraffic$/^{policy}$/^{shape}$',f'-test.benchtime={epochs}x','-test.benchmem','-test.count=1'],env=dict(os.environ,GC_CORE_POLICY=policy,GOMAXPROCS='1'),text=True,capture_output=True,cwd='/workspace/shared/wago-gc-eden-bench')
    row={'stage':stage,'round':rnd,'policy':policy,'shape':shape,'epochs':epochs,'start_utc':start,'end_utc':utc(),'start_loadavg':load,'end_loadavg':os.getloadavg(),'wall_seconds':time.monotonic()-t,'returncode':r.returncode,'stdout':r.stdout,'stderr':r.stderr};rows.append(row);(p/'results.json').write_text(json.dumps({'provenance':prov,'rows':rows},indent=2));print(stage,rnd,policy,shape,r.returncode,flush=True)
    if r.returncode or 'PASS' not in r.stdout:raise SystemExit('Failure retained')
