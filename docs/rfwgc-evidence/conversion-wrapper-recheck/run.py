import pathlib, subprocess, os, json, hashlib, tempfile, tarfile, io, collections
W=pathlib.Path('/home/dev/.t3/worktrees/wago/experiment-rfwgc-port'); E=W/'docs/rfwgc-evidence/conversion-wrapper-recheck'
def git(*args): return subprocess.check_output(['git',*args],cwd=W)
def sha(b): return hashlib.sha256(b).hexdigest()
def save(n,x): (E/n).write_text(json.dumps(x,indent=2)+'\n')
rev=git('rev-parse','5b02ec67a').decode().strip(); parent=git('rev-parse',rev+'^').decode().strip()
S=pathlib.Path(tempfile.mkdtemp(prefix='wago-conversion-recheck-',dir='/home/dev/.cache'))
archive=git('archive',rev); (S/'source.tar').write_bytes(archive)
R=S/'source'; R.mkdir()
with tarfile.open(fileobj=io.BytesIO(archive)) as t:t.extractall(R,filter='data')
links=[]
for line in git('ls-tree','-r',rev).decode().splitlines():
 if line.startswith('160000'):
  meta,path=line.split('\t'); pin=meta.split()[2]
  a=subprocess.check_output(['git','archive',pin],cwd=(pathlib.Path('/home/dev/Projects/wago') if path.endswith('spec-v3') else W)/path)
  with tarfile.open(fileobj=io.BytesIO(a)) as t:t.extractall(R/path,filter='data')
  links.append(dict(path=path,commit=pin,archive_sha256=sha(a)))
G='/home/dev/.local/share/mise/installs/go/1.27.1'; T='/home/dev/.cache/wago-final-7e41-82jf5iko/tooling/.tools/staticcheck/staticcheck'
prior=json.loads((W/'docs/rfwgc-evidence/final-combined-7e41/staticcheck-tool.json').read_text()); assert sha(pathlib.Path(T).read_bytes())==prior['sha256']
env=dict(os.environ,GOROOT=G,GOTOOLCHAIN='local',GOENV='off',GOWORK='off',GOMAXPROCS='4',GOFLAGS='-p=4',PATH=str(pathlib.Path(T).parent)+':'+G+'/bin:/usr/bin:/bin',CGO_ENABLED='0')
for k in ['GOOS','GOARCH']:env.pop(k,None)
save('identity.json',dict(commit=rev,parent=parent,tree=git('rev-parse',rev+'^{tree}').decode().strip(),archive_sha256=sha(archive),scratch=str(S),gitlinks=links,tool=prior,environment={k:env[k] for k in ['GOROOT','GOTOOLCHAIN','GOENV','GOWORK','GOMAXPROCS','GOFLAGS','PATH','CGO_ENABLED']}))
blobs=0; src_blobs=0; mismatches=[]
for line in git('ls-tree','-r',rev).decode().splitlines():
 meta,path=line.split('\t'); mode,kind,oid=meta.split()
 if kind!='blob': continue
 p=R/path; data=str(p.readlink()).encode() if mode=='120000' else p.read_bytes()
 actual=hashlib.sha1(b'blob '+str(len(data)).encode()+b'\0'+data).hexdigest()
 blobs+=1; src_blobs+=path.startswith('src/')
 if actual!=oid:mismatches.append(path)
save('archive-blob-check.json',dict(tracked_blobs=blobs,src_blobs=src_blobs,mismatches=mismatches))
assert not mismatches
(E/'status-before.txt').write_bytes(git('status','--porcelain=v1')); (E/'index-before.txt').write_bytes(git('diff','--cached','--raw'))
(E/'source.diff').write_bytes(git('diff',parent,rev,'--','src'))
(E/'calls-before.txt').write_bytes(git('grep','-n','-E',r'publishConversion(Store|Range)\b',parent,'--',':!docs'))
p=subprocess.run(['git','grep','-n','-E',r'publishConversion(Store|Range)\b',rev,'--',':!docs'],cwd=W,stdout=subprocess.PIPE); assert p.returncode==1
before=git('show',parent+':src/wago/design_repair_unsupported_test.go'); after=git('show',rev+':src/wago/design_repair_unsupported_test.go')
assert before.replace(b'.publishConversionRange(',b'.publishConversionRangeInvocation(gcConversionInvocation{}, ')==after
base='.just/staticcheck-baseline.tsv'; b=git('show',rev+':'+base); assert b==git('show',parent+':'+base)
(E/'prior-failed-staticcheck.log').write_bytes(git('show',rev+':docs/rfwgc-evidence/conversion-wrapper-cleanup/staticcheck.log'))
rows=[]
def run(name,args,extra={}):
 e=dict(env,**extra); log=E/(name+'.log')
 with log.open('w') as f: p=subprocess.run(args,cwd=R,env=e,stdout=f,stderr=subprocess.STDOUT)
 row=dict(name=name,command=args,environment_overrides=extra,exit=p.returncode,log_sha256=sha(log.read_bytes())); rows.append(row); save('commands.json',rows); print(name,p.returncode,flush=True)
 return log
run('go-version',[G+'/bin/go','version']);run('staticcheck-version',[T,'-version'])
run('staticcheck',[G+'/bin/go','run','./tests/tools/staticcheck-baseline','verify'])
for mode in ['ordinary','wagodebug','wago_regalloccheck']:
 tags=[] if mode=='ordinary' else ['-tags',mode]
 log=run(mode+'-host',[G+'/bin/go','test',*tags,'./src/wago','-run','Conversion|DesignRepair','-count=1','-json'])
 events=[]
 for line in log.read_text().splitlines():
  try:events.append(json.loads(line))
  except ValueError:pass
 tests=[x for x in events if x.get('Test') and x.get('Action') in ['pass','fail','skip']]
 save(mode+'-census.json',dict(counts=dict(collections.Counter(x['Action'] for x in tests)),tests=[{k:x[k] for k in ['Test','Action']} for x in tests]))
 for target in ['windows','darwin']:
  for arch in ['amd64','arm64']:
   run(mode+'-'+target+'-'+arch,[G+'/bin/go','test',*tags,'-c','./src/wago','-o',str(S/(mode+'-'+target+'-'+arch+'.test'))],dict(GOOS=target,GOARCH=arch))
assert (R/base).read_bytes()==b
save('result.json',dict(status='PASS' if all(r['exit']==0 for r in rows) else 'FAIL',baseline_sha256=sha(b),baseline_unchanged=True,test_transformation_exact=True,removed_symbol_references=0,commands=len(rows)))
