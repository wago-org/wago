#!/usr/bin/env python3
"""Correctness gates only; no benchmark or elapsed-time collection."""
import os,sys,pathlib,subprocess,json,hashlib,gzip
S=pathlib.Path(sys.argv[1]).resolve(); E=pathlib.Path(sys.argv[2]).resolve(); E.mkdir(parents=True,exist_ok=True)
GO='/home/dev/.local/share/mise/installs/go/1.27.1/bin/go'
env=os.environ.copy(); env.update(GOROOT=str(pathlib.Path(GO).parent.parent),GOTOOLCHAIN='local',GOMAXPROCS='2',GOFLAGS='-p=2',RFW_CATALOG=str(S/'corpus/catalog.json'),PATH='/home/dev/.local/share/mise/installs/node/24.22.2/bin:'+str(pathlib.Path(GO).parent)+':'+env['PATH'])
for k in ['WAGO_DEBUG_PANIC','WAGO_SHARED_SCALAR','GOWORK']:env.pop(k,None)
ledger=[]
def run(name,args,mode='',cwd=S,extra=None):
 d=E/name;d.mkdir(); e=env.copy();e.update(extra or {})
 cmd=[GO,'test','-json','-count=1','-timeout=60m']+(['-tags='+mode] if mode else [])+args
 (d/'command.json').write_text(json.dumps(dict(command=cmd,cwd=str(cwd),environment={k:v for k,v in e.items() if k in ["GOROOT","GOTOOLCHAIN","GOMAXPROCS","GOFLAGS","RFW_CATALOG","PATH","WAGO_SHARED_SCALAR"]}),indent=2)+'\n')
 with open(d/'raw.log','wb') as f: result=subprocess.run(cmd,cwd=cwd,env=e,stdout=f,stderr=subprocess.STDOUT)
 events=[]
 for line in (d/'raw.log').read_text().splitlines():
  try: j=json.loads(line)
  except ValueError:continue
  if j.get('Action') in ['pass','fail','skip']:events.append({k:j[k] for k in ['Action','Package','Test'] if k in j})
 raw=(d/'raw.log').read_bytes();(d/'raw.log.gz').write_bytes(gzip.compress(raw,mtime=0));(d/'raw.log').unlink()
 ledger.append(dict(name=name,exit=result.returncode,raw_sha256=hashlib.sha256(raw).hexdigest(),events=events))
 (E/'ledger.json').write_text(json.dumps(ledger,indent=2)+'\n');print(name,result.returncode,flush=True)
 return result.returncode
# The explicit no-inline/lifetime-analysis catalog is a compile-only safety gate.
for mode in ['wago_regalloccheck','','wagodebug']:
 if run('catalog-'+(mode or 'ordinary'),['-failfast','-run','^TestRFWStage3CorpusCompile$','./src/wago'],mode):sys.exit(1)
for mode in ['','wagodebug','wago_regalloccheck']:
 label=mode or 'ordinary'
 if run('focused-'+label,['-run','^(TestCommuteMemoryPreservesDestinationAddress|TestDeferredLoadAddress.*|TestRegallocCheck.*|TestScalarRouteRegionAndSelfUpdateContracts|TestIntervalRegion.*|TestCommuteSelfUpdate)$','./src/core/compiler/backend/railshot/amd64'],mode):sys.exit(1)
 if run('backend-'+label,['./src/core/compiler/backend/...'],mode):sys.exit(1)
 stats=','.join(filter(None,[mode,'wago_codegenstats']))
 if run('diagnostic-default-'+label,['./src/core/compiler/backend/...'],stats):sys.exit(1)
 if run('diagnostic-established-'+label,['./src/core/compiler/backend/...'],stats,extra={'WAGO_SHARED_SCALAR':'0'}):sys.exit(1)
for mode in ['','wagodebug','wago_regalloccheck']:
 if run('corpus-execution-'+(mode or 'ordinary'),['-failfast','-run','^(TestCorpus|TestCorpusSemanticExec|TestCatalog.*|TestValidateCorpusModule.*|TestApplicationCorpusRuns)$','./suite','-args','-wago.corpus=all'],mode,cwd=S/'bench'):sys.exit(1)
