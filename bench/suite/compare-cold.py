#!/usr/bin/env python3
"""One native main/default pair per input and phase; no precision repeats."""
import hashlib
import json
import math
import os
from pathlib import Path
import platform
import shutil
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[2]
BASE = "dc13730184e8ba6a1a2b3e3887c53a2b880db88c"
MANIFEST = ROOT / "bench/suite/testdata/cold-compilation/manifest.json"
OUTPUT = ROOT / ".validation/cold-comparison"

def run(args, cwd=ROOT, **kwargs):
    return subprocess.check_output(args, cwd=cwd, text=True, **kwargs).strip()

def main():
    if platform.system() not in ("Linux", "Darwin") or run(["go", "env", "GOARCH"]) != {"x86_64":"amd64","aarch64":"arm64","arm64":"arm64"}.get(platform.machine()):
        raise SystemExit("native Linux or Darwin Go target required")
    OUTPUT.mkdir(parents=True, exist_ok=True)
    env = {k:v for k,v in os.environ.items() if not k.startswith("WAGO_") and k not in ("GOGC", "GOMEMLIMIT", "GODEBUG")}
    env.update(GOMAXPROCS="1", WAGO_STUDY_MANIFEST=str(MANIFEST))
    identity = {"base":BASE,"candidate":run(["git","rev-parse","HEAD"]),"configuration":"defaults, one worker, GOMAXPROCS=1","go":run(["go","version"]),"cpu":run(["lscpu"] if platform.system()=="Linux" else ["sysctl","-n","machdep.cpu.brand_string"]),"kernel":platform.uname()._asdict(),"manifest_sha256":hashlib.sha256(MANIFEST.read_bytes()).hexdigest(),"protocol":"one alternating fresh-process pair per frozen input for compile and first-result phases; no code export before first result; small-shell fork excludes inherited launcher RSS"}
    inputs = json.loads(MANIFEST.read_text())["corpus"]
    samples = []
    with tempfile.TemporaryDirectory(prefix="wago-cold-") as temporary:
        temporary = Path(temporary)
        base = temporary / "base"
        subprocess.run(["git","worktree","add","--detach",str(base),BASE],cwd=ROOT,check=True)
        try:
            for rel in ("bench/suite/optimization_study_test.go","src/wago/compile_review_bench_test.go"):
                shutil.copyfile(ROOT / rel, base / rel)
            binaries = {}
            for image, source in (("base",base),("candidate",ROOT)):
                binaries[image] = temporary / (image+".test")
                subprocess.run(["go","test","-c","-o",str(binaries[image]),"./suite"],cwd=source/"bench",env=env,check=True)
                small = temporary / (image+"-small.test")
                subprocess.run(["go","test","-c","-o",str(small),"./src/wago"],cwd=source,env=env,check=True)
                binaries[image+"-small"] = small
                identity[image+"_binary_sha256"] = hashlib.sha256(binaries[image].read_bytes()).hexdigest()
            if platform.system() == "Linux":
                cpu = min(os.sched_getaffinity(0))
                os.sched_setaffinity(0,{cpu})
                identity["affinity_cpu"] = cpu
            else:
                identity["affinity_cpu"] = None # Darwin has no sched_setaffinity
            (OUTPUT/"identity.json").write_text(json.dumps(identity,indent=2)+"\n")
            def launch(binary,args,cwd,sample_env):
                return run(["/bin/sh","-c",'"$@" & child=$!; wait "$child"',"cold-launch",str(binary),*args],cwd=cwd,env=sample_env,stderr=subprocess.STDOUT,timeout=240)
            with (OUTPUT/"raw.log").open("w") as raw, (OUTPUT/"samples.jsonl").open("w") as receipt:
                for phase in ("compile","first-result"):
                    for index, row in enumerate(inputs):
                        for image in (("base","candidate") if index%2==0 else ("candidate","base")):
                            sample_env = dict(env,WAGO_STUDY_PHASE=phase)
                            log = launch(binaries[image],["-test.run=^TestOptimizationStudySample$","-test.v","-test.timeout=3m","-wago.corpus="+row["id"]],ROOT/"bench/suite",sample_env)
                            raw.write(f"{phase} {image} {row['id']}\n{log}\n");raw.flush()
                            rows = [json.loads(l[13:]) for l in log.splitlines() if l.startswith("STUDY_SAMPLE ")]
                            if len(rows)!=1 or rows[0]["oracle"]!="pass" or rows[0]["sha256"]!=row["sha256"]:
                                raise ValueError("invalid sample")
                            sample = dict(rows[0],image=image)
                            samples.append(sample);receipt.write(json.dumps(sample)+"\n");receipt.flush()
                for image in ("base","candidate"):
                    log = launch(binaries[image+"-small"],["-test.run=^$","-test.bench=^BenchmarkCompileSmallModule$","-test.benchtime=10000x","-test.count=1"],ROOT/"src/wago",env)
                    raw.write(f"SMALL {image}\n{log}\n")
                    (OUTPUT/(image+"-small.txt")).write_text(log+"\n")
            summary = {}
            for phase in ("compile","first-result"):
                groups = {image:{r["id"]:r for r in samples if r["phase"]==phase and r["image"]==image} for image in ("base","candidate")}
                a,b = groups["base"],groups["candidate"]
                metrics = ("compile_ns","compile_peak_rss_bytes","allocated_bytes") if phase=="compile" else ("first_result_ns","first_result_peak_rss_bytes","pipeline_allocated_bytes")
                summary[phase] = {"pairs":len(a),"oracles_passed":len(a)+len(b),"native_hashes_equal":sum(a[k]["native_code_sha256"]==b[k]["native_code_sha256"] for k in a),"ratios":{m:sum(r[m] for r in b.values())/sum(r[m] for r in a.values()) for m in metrics},"per_input":{k:{m:b[k][m]/a[k][m] for m in metrics} for k in a}}
                if phase=="compile":summary[phase]["execution_geomean_ratio"]=math.exp(sum(math.log(b[k]["exec_ns"]/a[k]["exec_ns"]) for k in a)/len(a))
            (OUTPUT/"summary.json").write_text(json.dumps(summary,indent=2)+"\n")
            print(json.dumps(summary,indent=2))
        finally:
            subprocess.run(["git","worktree","remove","--force",str(base)],cwd=ROOT,check=True)

if __name__ == "__main__":
    main()
