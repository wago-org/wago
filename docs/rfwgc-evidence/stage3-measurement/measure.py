#!/usr/bin/env python3
"""Independent worker only. Requires explicit grant, settled correctness, and global lock.
Never run as part of preparation or correctness validation.
"""
import argparse,fcntl,hashlib,json,os,pathlib,re,subprocess,time
HERE=pathlib.Path(__file__).resolve().parent
LOCK=pathlib.Path("/home/dev/Projects/wago/.performance-measurement.lock")
def thermal():
 rows=[]
 for p in pathlib.Path("/sys/class/hwmon").glob("hwmon*/temp*_input"):
  if (p.parent/"name").read_text().strip() in ("coretemp","k10temp","cpu_thermal"):
   rows.append({"path":str(p),"c":int(p.read_text())/1000})
 if not rows:raise RuntimeError("No CPU temperature sensor; fail closed")
 return rows
def telemetry():
 return {"utc_ns":time.time_ns(),"thermal":thermal(),
         "proc_stat":pathlib.Path("/proc/stat").read_text(),
         "loadavg":pathlib.Path("/proc/loadavg").read_text(),
         "pressure":pathlib.Path("/proc/pressure/cpu").read_text()}
def maximum(rows):return max(x["c"] for x in rows)
def metric(text):
 rows=[line.split() for line in text.splitlines() if line.startswith("Benchmark") and "ns/op" in line]
 if len(rows)!=1:raise RuntimeError("Expected exactly one benchmark leaf")
 return float(rows[0][rows[0].index("ns/op")-1])
def main():
 ap=argparse.ArgumentParser()
 ap.add_argument("--prepared",type=pathlib.Path,required=True)
 ap.add_argument("--out",type=pathlib.Path,required=True)
 ap.add_argument("--grant",type=pathlib.Path,required=True)
 ap.add_argument("--cpu",type=int,required=True)
 ap.add_argument("--plan-only",action="store_true")
 ap.add_argument("--comparison",choices=["revision","region"],required=True)
 a=ap.parse_args()
 grant=json.loads(a.grant.read_text())
 assert grant["status"]=="granted" and grant["correctness_complete"] is True
 assert grant["workers_quiesced"] is True and grant["independent_measurer"] is True
 assert grant["owner"] and grant["authorization"] and grant["host"]==os.uname().nodename
 # Do not delete/rewrite legacy lease records. Their owner must resolve them.
 for p in pathlib.Path("/tmp").glob("*measurement.lease"):
  r=json.loads(p.read_text())
  if r.get("status")=="active":raise RuntimeError(f"Unresolved active lease: {p}")
 a.prepared=a.prepared.resolve();a.out=a.out.resolve()
 identity=json.loads((a.prepared/"identity.json").read_text())
 for name,digest in identity["binaries"].items():
  assert hashlib.sha256((a.prepared/name).read_bytes()).hexdigest()==digest
 cells=[("native","BenchmarkRFWStage3Native",n) for n in ["scalar","graph","low","high","promotion","large"]]
 cells += [("guest",family,n) for family in ["BenchmarkRFWStage3Guest","BenchmarkRFWStage3Compile"] for n in ["noGC","scalar","graph"]]
 plan={"identity":identity,"grant":grant,"comparison":a.comparison,"cells":cells,"rounds":6,
       "order":"ABBA then BAAB alternating","control_range":[.90,1.10],"cpu":a.cpu,
       "environment":{k:os.environ.get(k) for k in ["GOGC","GOMEMLIMIT","GOEXPERIMENT","GODEBUG"]},
       "uname":list(os.uname()),"start_max_c":60,"discard_at_c":85,"benchtime":"500ms","max_block_attempts":3}
 if a.plan_only:
  a.out.mkdir()
  (a.out/"plan.json").write_text(json.dumps(plan,indent=2)+"\n")
  return
 assert json.loads((a.out/"plan.json").read_text())==plan,"freeze/commit matching plan first with --plan-only"
 assert not (a.out/"telemetry.jsonl").exists(),"no silent resume; retain failed campaign and freeze a new one"
 def log(row):
  with (a.out/"telemetry.jsonl").open("a") as f:f.write(json.dumps(row)+"\n")
 def sample(package,family,name,side,label):
  pin=("baseline" if side=="A" else "port") if a.comparison=="revision" else "port"
  region="On" if a.comparison=="region" and side=="B" else "Off"
  binary=a.prepared/f"{pin}-ordinary-{package}.test"
  deadline=time.monotonic()+300
  while True:
   obs=telemetry();log({"event":"admission","label":label,**obs})
   if maximum(obs["thermal"])<=60:break
   if time.monotonic()>deadline:raise RuntimeError("cooldown deadline; stop, do not relax gate")
   time.sleep(2)
  cmd=["taskset","-c",str(a.cpu),str(binary),"-test.run=^$","-test.bench=^"+family+"$/^"+name+"$",
       "-test.benchtime=500ms","-test.count=1","-test.v","-test.timeout=3m"]
  raw=a.out/(label+".txt");severe=False
  env=os.environ|{"GOMAXPROCS":"1","RFW_REGION":region}
  with raw.open("w") as f:
   p=subprocess.Popen(cmd,stdout=f,stderr=subprocess.STDOUT,env=env,cwd=a.prepared/pin/("src/wago" if package=="guest" else "src/core/runtime/gc/native"))
   try:
    while p.poll() is None:
     obs=telemetry();log({"event":"during","label":label,**obs})
     severe |= maximum(obs["thermal"])>=85
     time.sleep(.25) # Monitor only: child runs continuously.
   finally:
    if p.poll() is None:p.terminate();p.wait()
  obs=telemetry();severe |= maximum(obs["thermal"])>=85
  log({"event":"finish","label":label,"command":cmd,"requested_region":region,"effective_region":("not-applicable" if family=="BenchmarkRFWStage3Compile" else "Off" if package=="guest" and name=="noGC" else region),"returncode":p.returncode,"severe":severe,**obs})
  return None if severe or p.returncode else metric(raw.read_text())
 with LOCK.open("a+") as lease:
  fcntl.flock(lease,fcntl.LOCK_EX|fcntl.LOCK_NB)
  log({"event":"lease-acquired","pid":os.getpid(),"path":str(LOCK)})
  try:
   for package,family,name in cells:
    for round_ in range(6):
     order="ABBA" if round_%2==0 else "BAAB"
     for attempt in range(3):
      values={"A":[],"B":[]}
      for index,side in enumerate(order):
       label=f"{family}-{name}-{round_}-{attempt}-{index}-{side}"
       values[side].append(sample(package,family,name,side,label))
      valid=all(x is not None for xs in values.values() for x in xs)
      controls={side:xs[1]/xs[0] for side,xs in values.items()} if valid else {}
      accepted=valid and all(.90<=x<=1.10 for x in controls.values())
      log({"event":"block","cell":[family,name],"round":round_,"attempt":attempt,
           "values":values,"controls":controls,"accepted":accepted})
      if accepted:break
     else:raise RuntimeError("three rejected blocks; reassess without cherry-picking")
  finally:
   log({"event":"lease-released","pid":os.getpid()})
   fcntl.flock(lease,fcntl.LOCK_UN)
if __name__=="__main__":main()
