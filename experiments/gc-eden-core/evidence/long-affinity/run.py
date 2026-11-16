import subprocess,os,json,pathlib,time,datetime,hashlib,platform
p=pathlib.Path('/workspace/shared/gc-eden-core-long');policies=['promote','age2','eden','eden-same-fresh'];shapes=['dead-heavy','persistent344KiB','array-bulk','rolling84x4096','dense-persistent','old-young-mutation'];rows=[]; selected_cpu=min(os.sched_getaffinity(0))
def utc():return datetime.datetime.now(datetime.timezone.utc).isoformat()
prov={'source':'b893312c','harness_source':'f2606161','binary_sha256':hashlib.sha256((p/'core.test').read_bytes()).hexdigest(),'platform':platform.platform(),'cpu':pathlib.Path('/proc/cpuinfo').read_text(),'affinity':sorted(os.sched_getaffinity(0)),'selected_cpu':selected_cpu,'affinity_method':'taskset -c inherited by every fresh process','protocol_change':'128 epochs and pinned CPU; no source/runtime.GC change; do not pool absolute times with unpinned8epoch run','tags':['wago_gc_compact_eden','wago_gc_compact_age2'],'protocol':'24 audits1x then5 rotating rounds fixed128epochs, core-only same-source'}
for stage,rounds,epochs in [('audit',1,1),('timed',5,128)]:
 for rnd in range(rounds):
  for sj in range(len(shapes)):
   shape=shapes[(sj+rnd)%len(shapes)]
   for pj in range(4):
    policy=policies[(pj+rnd)%4];t=time.monotonic();start=utc();load=os.getloadavg()
    r=subprocess.run(['taskset','-c',str(selected_cpu),str(p/'core.test'),'-test.run=^$',f'-test.bench=^BenchmarkEdenTraffic$/^{policy}$/^{shape}$',f'-test.benchtime={epochs}x','-test.benchmem','-test.count=1'],env=dict(os.environ,GC_CORE_POLICY=policy,GOMAXPROCS='1'),text=True,capture_output=True,cwd='/workspace/shared/wago-gc-eden-bench')
    row={'stage':stage,'round':rnd,'policy':policy,'shape':shape,'epochs':epochs,'start_utc':start,'end_utc':utc(),'start_loadavg':load,'selected_cpu':selected_cpu,'parent_allowed_affinity':sorted(os.sched_getaffinity(0)),'end_loadavg':os.getloadavg(),'wall_seconds':time.monotonic()-t,'returncode':r.returncode,'stdout':r.stdout,'stderr':r.stderr};rows.append(row);(p/'results.json').write_text(json.dumps({'provenance':prov,'rows':rows},indent=2));print(stage,rnd,policy,shape,r.returncode,flush=True)
    if r.returncode or 'PASS' not in r.stdout:raise SystemExit('Failure retained')
