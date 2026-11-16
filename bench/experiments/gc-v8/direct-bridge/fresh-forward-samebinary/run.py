import pathlib,os,subprocess,json,datetime,time,hashlib,argparse
ap=argparse.ArgumentParser();ap.add_argument("--repo",type=pathlib.Path,required=True);ap.add_argument("--binary",type=pathlib.Path,required=True);ap.add_argument("--output",type=pathlib.Path,required=True);args=ap.parse_args()
out=args.output.resolve();binary=args.binary.resolve();out.mkdir(parents=True,exist_ok=True);sha=hashlib.sha256(binary.read_bytes()).hexdigest();assert sha=='a76400feceef5dbcfac8c175e8bc16f50648800b398b0aef4ca39270f3d2470f'
root=args.repo.resolve();manifest={c['name']:c for c in json.load(open(root/'bench/experiments/gc-v8/cases.json'))};cpu=min(os.sched_getaffinity(0));rows=[]
def utc():return datetime.datetime.now(datetime.timezone.utc).isoformat()
prov={'binary_sha256':sha,'source':'1e9dba8b','qualification':'a9750059','protocol':'same binary explicit0/1;8adjacent reversed pairs each struct/fill;100warmup9samples64calls; affinity-controlled diagnostic, not V8gate','cpu':pathlib.Path('/proc/cpuinfo').read_text(),'selected_cpu':cpu,'allowed_affinity':sorted(os.sched_getaffinity(0)),'started_utc':utc()}
for rnd in range(8):
 cases=['struct-allocation-access','array-fill'] if rnd%2==0 else ['array-fill','struct-allocation-access']
 for case in cases:
  order=['0','1'] if rnd%2==0 else ['1','0']
  for mode in order:
   start=utc();load=os.getloadavg();tick=time.monotonic()
   env=dict(os.environ,GOMAXPROCS='1',GC_BRIDGE_CASE=case,GC_BRIDGE_MODE='direct',GC_BRIDGE_COMPILER_MODE='samebinary-'+mode,GC_BRIDGE_FRESH_STRUCT_FORWARD=mode,GC_BRIDGE_AGE2='0',GC_BRIDGE_EDEN='2097152',GC_BRIDGE_WARMUP='100',GC_BRIDGE_SAMPLES='9',GC_BRIDGE_BATCH='64',GC_BRIDGE_ALLOW_ZERO_COLLECTION='0')
   p=subprocess.run(['taskset','-c',str(cpu),str(binary),'-test.run=^TestGCDirectBridgeProbe$','-test.v','-test.count=1'],cwd=root/'src/wago',env=env,text=True,capture_output=True)
   r={'round':rnd,'case':case,'cache':mode,'order':order,'started_utc':start,'ended_utc':utc(),'start_loadavg':load,'end_loadavg':os.getloadavg(),'wall_seconds':time.monotonic()-tick,'selected_cpu':cpu,'returncode':p.returncode,'stdout':p.stdout,'stderr':p.stderr}
   for line in p.stdout.splitlines():
    if 'DIRECT_BRIDGE_RESULT ' in line:r['result']=json.loads(line.split('DIRECT_BRIDGE_RESULT ',1)[1])
   rows.append(r);(out/'results.json').write_text(json.dumps({'provenance':prov,'rows':rows},indent=2));print(rnd,case,mode,p.returncode,flush=True)
   assert p.returncode==0 and 'result' in r
   s=r['result'];assert s['fresh_struct_forward_override']==mode and s['checksum']==manifest[case]['want'] and s['artifact_sha256']==manifest[case]['sha256'] and s['native_direct_view_installed'] and s['mode']=='direct'
   assert not s['allocation_elision'] and not s['config']['ExperimentalCompactAge2'] and not s['config']['ExperimentalCompactOffsets']
