#!/usr/bin/env python3
"""Pinned corpus correctness only; no benchmark or timing acquisition."""
import hashlib, io, json, os, pathlib, subprocess, tarfile
HERE=pathlib.Path(__file__).resolve().parent
ROOT=HERE.parents[2]
PINS={'baseline':'a3c08365f531eb3c9003e3acf3e0a822c12c31f4','port':'5b31c81ec0c74695a90bc8d36754b92845b59f14'}
GO='/home/dev/.local/share/mise/installs/go/1.27.1/bin/go'
def sha(p): return hashlib.sha256(p.read_bytes()).hexdigest()
def main():
 scratch=HERE/'.exports';scratch.mkdir()
 env=os.environ|{'GOROOT':str(pathlib.Path(GO).parents[1]),'GOTOOLCHAIN':'local','GOMAXPROCS':'2','GOFLAGS':'-p=2','GOWORK':'off','GOCACHE':str(scratch/'go-cache'),'WAGO_APP_CORPUS_REFERENCE':'all','WAGO_CORPUS_TIMEOUT':'1m'}
 for key in ['WAGO_APP_CORPUS_SHARD','WAGO_APP_CORPUS_SHARDS','WAGO_APP_CORPUS_REPORT','WAGO_CORPUS_CHILD']:env.pop(key,None)
 records=[];identity={'pins':PINS,'go':subprocess.check_output([GO,'version'],env=env,text=True).strip(),'environment':{k:env[k] for k in ['GOROOT','GOTOOLCHAIN','GOMAXPROCS','GOFLAGS','GOWORK','GOCACHE','WAGO_APP_CORPUS_REFERENCE','WAGO_CORPUS_TIMEOUT']},'catalogs':{}}
 for pin,revision in PINS.items():
  tree=scratch/pin;tree.mkdir()
  with tarfile.open(fileobj=io.BytesIO(subprocess.check_output(['git','archive',revision],cwd=ROOT))) as tf:tf.extractall(tree,filter='data')
  cat=json.loads((tree/'corpus/catalog.json').read_text()); rows=[]
  for e in cat['benchmarks']:
   p=tree/'corpus'/e['artifact']; inputs=[]
   for name,want in e.get('command',{}).get('inputs',{}).items():
    q=tree/'corpus'/e['command']['preopen']/name
    inputs.append({'path':str(q.relative_to(tree)),'expected':want,'actual':sha(q) if q.is_file() else None})
   rows.append({'id':e['id'],'artifact':e['artifact'],'expected':e['artifact_sha256'],'actual':sha(p) if p.is_file() else None,'inputs':inputs})
  identity['catalogs'][pin]={'sha256':sha(tree/'corpus/catalog.json'),'rows':rows}
  (HERE/'identity.json').write_text(json.dumps(identity,indent=2)+'\n')
  assert all(r['actual']==r['expected'] and all(i['actual']==i['expected'] for i in r['inputs']) for r in rows)
  for mode in ['ordinary','wagodebug','wago_regalloccheck']:
   cmd=[GO,'test','-json','-count=1','-timeout=60m']+([] if mode=='ordinary' else ['-tags='+mode])+['./suite','-run','^(TestCorpus|TestCorpusSemanticExec|TestApplicationCorpusRuns)$','-args','-wago.corpus=all']
   name=f'{pin}-{mode}'
   (HERE/(name+'.command.json')).write_text(json.dumps({'cwd':str(tree/'bench'),'command':cmd,'environment':identity['environment']},indent=2)+'\n')
   print('RUN',name,flush=True)
   with (HERE/(name+'.jsonl')).open('w') as f, (HERE/(name+'.stderr.log')).open('w') as err:
    p=subprocess.run(cmd,cwd=tree/'bench',env=env,stdout=f,stderr=err)
   records.append({'pin':pin,'revision':revision,'mode':mode,'exit':p.returncode,'raw':name+'.jsonl'})
   (HERE/'results.json').write_text(json.dumps(records,indent=2)+'\n')
   print('DONE',name,p.returncode,flush=True)
 # Propagate recorded child failures to callers; does not change test commands.
 raise SystemExit(any(r['exit'] for r in records))
if __name__=='__main__':main()
