#!/usr/bin/env python3
"""Export immutable pins inside the owned directory, overlay identical test harnesses.
No benchmark invocation. Run once into a NEW directory; remove only that directory.
"""
import hashlib, io, json, os, pathlib, shutil, subprocess, sys, tarfile
HERE=pathlib.Path(__file__).resolve().parent
ROOT=HERE.parents[2]
PINS={"baseline":"a3c08365f531eb3c9003e3acf3e0a822c12c31f4",
      "port":"5b31c81ec0c74695a90bc8d36754b92845b59f14"}
HARNESS=["src/wago/rfwgc_stage3_measurement_bench_test.go",
         "src/core/runtime/gc/native/rfwgc_stage3_measurement_bench_test.go"]
def main():
 out=pathlib.Path(sys.argv[1]).resolve()
 out.relative_to(HERE);out.mkdir()
 go=os.environ.get("RFW_GO","/home/dev/.local/share/mise/installs/go/1.27.1/bin/go")
 env=os.environ|{"GOROOT":str(pathlib.Path(go).parents[1]),"GOTOOLCHAIN":"local","GOMAXPROCS":"2","GOFLAGS":"-p=2","GOWORK":"off"}
 record={"pins":PINS,"harness":{},"binaries":{},"go":subprocess.check_output([go,"version"],text=True).strip()}
 for rel in HARNESS:record["harness"][rel]=hashlib.sha256((ROOT/rel).read_bytes()).hexdigest()
 for label,pin in PINS.items():
  tree=out/label;tree.mkdir()
  with tarfile.open(fileobj=io.BytesIO(subprocess.check_output(["git","archive",pin],cwd=ROOT))) as tf:tf.extractall(tree,filter="data")
  for rel in HARNESS:shutil.copyfile(ROOT/rel,tree/rel)
  for mode in ["ordinary","wagodebug","wago_regalloccheck"]:
   for package,path in [("guest","./src/wago"),("native","./src/core/runtime/gc/native")]:
    binary=out/f"{label}-{mode}-{package}.test"
    cmd=[go,"test","-c","-o",str(binary)]+([] if mode=="ordinary" else ["-tags="+mode])+[path]
    with (out/f"{binary.name}.build.log").open("w") as f:subprocess.run(cmd,cwd=tree,env=env,stdout=f,stderr=subprocess.STDOUT,check=True)
    record["binaries"][binary.name]=hashlib.sha256(binary.read_bytes()).hexdigest()
    with (out/f"{binary.name}.validate.log").open("w") as f:subprocess.run([str(binary),"-test.run=^TestRFWStage3Measurement","-test.v","-test.count=1"],cwd=tree/path,env=env,stdout=f,stderr=subprocess.STDOUT,check=True)
 (out/"identity.json").write_text(json.dumps(record,indent=2)+"\n")
if __name__=="__main__":main()
