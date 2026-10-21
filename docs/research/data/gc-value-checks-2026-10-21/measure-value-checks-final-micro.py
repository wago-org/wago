from pathlib import Path
import subprocess,time,json,os,re,statistics
r=Path('/home/dev/Projects/dewdrop');out=r/'.tmp/wago-next-gc-perf/value-checks/micro-final';tree=r/'.tmp/wago-gc-value-checks/src/wago';env=dict(os.environ,CGO_ENABLED='0',GOMAXPROCS='1',GOWORK='off');rows=[]
for i,variant in enumerate(['control','candidate','candidate','control']):
 cmd=['taskset','-c','15',str(out/(variant+'.test')),'-test.run=^TestGCValueChecks','-test.bench=^BenchmarkGCValueChecks$','-test.benchtime=200ms','-test.count=1'];t=time.monotonic();p=subprocess.run(cmd,cwd=tree,env=env,capture_output=True,text=True);row=dict(variant=variant,command=cmd,seconds=time.monotonic()-t,exit=p.returncode,stdout=p.stdout,stderr=p.stderr);matches=re.findall(r'(BenchmarkGCValueChecks)\s+\d+\s+([0-9.]+) ns/op',p.stdout)
 row['ns_per_8192_reads']={name:float(value) for name,value in matches}
 rows.append(row);(out/'samples.json').write_text(json.dumps(rows,indent=2));print(json.dumps(row),flush=True)
 if p.returncode:raise SystemExit(p.returncode)
summary={}
for entry in ['BenchmarkGCValueChecks']:
 values={v:statistics.median(x['ns_per_8192_reads'][entry] for x in rows if x['variant']==v) for v in ['control','candidate']}
 values['reduction_percent']=100*(1-values['candidate']/values['control']);summary[entry]=values
(out/'summary.json').write_text(json.dumps(summary,indent=2));print(json.dumps(summary),flush=True)
