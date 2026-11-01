#!/usr/bin/env python3
"""Future independent correctness gate, no benchmarks. Existing canonical oracles.
No claims for unsupported/skipped rows; keep raw Go JSON and every return code.
"""
import hashlib,json,os,pathlib,subprocess,sys
prepared=pathlib.Path(sys.argv[1]).resolve()
out=pathlib.Path(sys.argv[2]).resolve();out.mkdir()
go=os.environ.get("RFW_GO","/home/dev/.local/share/mise/installs/go/1.27.1/bin/go")
env=os.environ|{"GOROOT":str(pathlib.Path(go).parents[1]),"GOTOOLCHAIN":"local",
 "GOMAXPROCS":"2","GOFLAGS":"-p=2","GOWORK":"off","WAGO_APP_CORPUS_REFERENCE":"wago-only"}
records=[];catalog=None
for pin in ["baseline","port"]:
 tree=prepared/pin
 data=(tree/"corpus/catalog.json").read_bytes()
 digest=hashlib.sha256(data).hexdigest()
 if catalog is None:catalog=digest
 assert catalog==digest,"catalog differs: freeze common input before gate"
 for mode in ["ordinary","wagodebug","wago_regalloccheck"]:
  cmd=[go,"test","-json","-count=1","-timeout=60m"]
  if mode!="ordinary":cmd+=["-tags="+mode]
  cmd+=["./suite","-run","^(TestCorpus|TestCorpusSemanticExec|TestApplicationCorpusRuns)$","-args","-wago.corpus=all"]
  path=out/f"{pin}-{mode}.jsonl"
  with path.open("w") as f:
   p=subprocess.run(cmd,cwd=tree/"bench",env=env,stdout=f,stderr=subprocess.STDOUT)
  records.append({"pin":pin,"mode":mode,"command":cmd,"exit":p.returncode,"raw":path.name})
(out/"results.json").write_text(json.dumps({"catalog_sha256":catalog,"runs":records,
 "reference_scope":"Wago exact catalog/oracle checks; external runtime differentials NOT run",
 "denominator":"Join every catalog ID to raw subtests; missing/skip is NOT pass"},indent=2)+"\n")
sys.exit(any(r["exit"] for r in records))
