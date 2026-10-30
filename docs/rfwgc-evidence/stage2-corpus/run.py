#!/usr/bin/env python3
"""Full catalog, archived revisions, existing oracle/benchmark adapters. No profile builds."""
import pathlib,subprocess,os,json,tempfile,tarfile,io,hashlib,sys,time
ROOT=pathlib.Path(__file__).resolve().parents[3]
OUT=pathlib.Path(__file__).resolve().parent
BASE='6ceb862219e64480c0503836b19ee1e02b209cc7'
PORT='9d7e4fdf7925265ee0c7db9246e66f0707ee2d76'
GO='/home/dev/.local/share/mise/installs/go/1.27.1/bin/go'
def digest(p): return hashlib.sha256(p.read_bytes()).hexdigest()
def run(cmd,cwd,env,path):
 p=subprocess.run(cmd,cwd=cwd,env=env,stdout=subprocess.PIPE,stderr=subprocess.STDOUT)
 path.write_bytes(p.stdout)
 return p.returncode

def prepare(tmp):
 env=os.environ.copy();env.update(GOROOT=str(pathlib.Path(GO).parents[1]),GOMAXPROCS='1',GOTOOLCHAIN='local',GOWORK='off',GOFLAGS='',GOEXPERIMENT='',GOCACHE=str(tmp/'cache'),GOTMPDIR=str(tmp));env.pop('WAGO_EXPLAIN',None)
 cat=json.loads((ROOT/'corpus/catalog.json').read_text())
 manifest=[]
 for m in cat['benchmarks']:
  p=ROOT/'corpus'/m['artifact']; row=dict(m);row['actual_sha256']=digest(p) if p.exists() else None
  row['artifact_present']=p.exists();row['hash_matches']=row['actual_sha256']==m['artifact_sha256'];manifest.append(row)
 (OUT/'manifest.json').write_text(json.dumps({'catalog_sha256':digest(ROOT/'corpus/catalog.json'),'benchmarks':manifest,'checks':cat['checks']},indent=2)+'\n')
 for name,ref in [('baseline',BASE),('stage2',PORT)]:
  tree=tmp/name;tree.mkdir()
  with tarfile.open(fileobj=io.BytesIO(subprocess.check_output(['git','archive',ref],cwd=ROOT))) as tf: tf.extractall(tree,filter='data')
  # Both archives consume precisely the current catalog, artifacts, inputs and adapters.
  import shutil
  shutil.rmtree(tree/'corpus');shutil.copytree(ROOT/'corpus',tree/'corpus')
  shutil.rmtree(tree/'bench');shutil.copytree(ROOT/'bench',tree/'bench',ignore=shutil.ignore_patterns('.git','*.test'))
  p=tree/'bench/internal/semanticcorpus/run.go';p.write_text(p.read_text().replace('wago.Compile(nil, wasm)', 'wago.Compile(wago.NewRuntimeConfig().WithOptimization("gc-lifetime-analyze", os.Getenv("STAGE2_ON") == "1"), wasm)'))
  suite=tree/'bench/suite';(suite/'stage2_corpus_test.go').write_text((OUT/'adapter_test.go.txt').read_text())
  for f in ['suite_test.go','command_corpus_test.go']:
   p=suite/f;s=p.read_text();s=s.replace('cfg := wago.NewRuntimeConfig()','cfg := stage2Config()').replace('wago.Compile(nil, m.bytes)','wago.Compile(stage2Config(), m.bytes)');p.write_text(s)
  if name=='stage2':
   (suite/'stage2_inventory_test.go').write_text((OUT/'inventory_test.go.txt').read_text())
   backend=tree/'src/core/compiler/backend/railshot/amd64';(backend/'corpus_stats.go').write_text((OUT/'stats.go.txt').read_text())
   p=backend/'compile.go';p.write_text(p.read_text().replace('ms.String()', 'corpusStats(ms)'))
  for tags in ['', 'wagodebug','wago_regalloccheck']:
   target=tmp/(name+('-'+tags if tags else '')+'.test')
   cmd=[GO,'test','-p=1','-c','-o',str(target)]+(['-tags='+tags] if tags else [])+['./suite']
   if run(cmd,tree/'bench',env,OUT/(name+'-build'+('-'+tags if tags else '')+'.txt')):raise RuntimeError('build failed')
  if name=='stage2':
   if run([GO,'test','-p=1','-c','-tags=wago_codegenstats','-o',str(tmp/'stats.test'),'./suite'],tree/'bench',env,OUT/'stats-build.txt'):raise RuntimeError('stats build failed')
 return env,manifest

if __name__=='__main__' and '--resume' not in sys.argv:
 with tempfile.TemporaryDirectory(prefix='wago-stage2-corpus-') as td:
  tmp=pathlib.Path(td);(OUT/'temporary-directory.txt').write_text(td+'\n')
  env,manifest=prepare(tmp)
  # Serial isolated public compile attempts retain exact failures without losing catalog coverage.
  stats=[]
  for n,m in enumerate(manifest):
   id=m['id']; e=env|{'STAGE2_ON':'1','WAGO_EXPLAIN':'1'}
   rc=run([str(tmp/'stats.test'),'-test.run=^TestStage2(CorpusCompile|Inventory)$','-test.v','-wago.corpus='+id],tmp/'stage2/bench/suite',e,OUT/('static-'+id+'.txt'))
   stats.append({'id':id,'returncode':rc});print('static',n+1,len(manifest),id,rc,flush=True)
  (OUT/'static-outcomes.json').write_text(json.dumps(stats,indent=2)+'\n')
  # Validation invokes every eligible adapter once, including exact oracle checks.
  for name in ['baseline','stage2']:
   for tags in ['', 'wagodebug','wago_regalloccheck']:
    binary=tmp/(name+('-'+tags if tags else '')+'.test')
    for m in manifest:
     rc=run([str(binary),'-test.run=^$','-test.bench=^(BenchmarkExec|BenchmarkCommandExec)$','-test.benchtime=1x','-test.timeout=2m','-wago.corpus='+m['id']],tmp/name/'bench/suite',env|{'STAGE2_ON':str(int(name=='stage2'))},OUT/('validate-'+name+'-'+(tags or 'ordinary')+'-'+m['id']+'.txt'))
     print('validate',name,tags or 'ordinary',m['id'],rc,flush=True)
  subprocess.run(['git','-C','/home/dev/Projects/slipstream','fetch','origin','master'],stdout=open(OUT/'mirror-pre-timing.txt','w'),stderr=subprocess.STDOUT)
  import measure
  measure.main(tmp,env,manifest)
 (OUT/'cleanup.txt').write_text('Temporary archives, binaries, GOTMPDIR and private GOCACHE removed: '+td+'\n')

if __name__=='__main__' and '--resume' in sys.argv:
 tmp=pathlib.Path(sys.argv[sys.argv.index('--resume')+1]).resolve()
 env=os.environ.copy();env.update(GOROOT=str(pathlib.Path(GO).parents[1]),GOMAXPROCS='1',GOTOOLCHAIN='local',GOWORK='off',GOFLAGS='',GOEXPERIMENT='',GOCACHE=str(tmp/'cache'),GOTMPDIR=str(tmp));env.pop('WAGO_EXPLAIN',None)
 manifest=json.loads((OUT/'manifest.json').read_text())['benchmarks']
 import measure
 measure.main(tmp,env,manifest,resume=True)
