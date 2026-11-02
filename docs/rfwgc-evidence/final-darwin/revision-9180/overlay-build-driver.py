import pathlib,tempfile,subprocess,tarfile,os,json,hashlib
root=pathlib.Path.cwd(); h=root/'docs/rfwgc-evidence/final-darwin'; rev=h/'revision-9180'; spec=json.loads((rev/'contracts.json').read_text());rows=[]
with tempfile.TemporaryDirectory(prefix='rfwgc-contract-build-') as tmp:
 tmp=pathlib.Path(tmp);src=tmp/'source';src.mkdir()
 archive=tmp/'source.tar'
 with archive.open('wb') as f: subprocess.run(['git','archive',spec['source'],'--','.',':!docs'],stdout=f,check=True)
 with tarfile.open(archive) as f:f.extractall(src,filter='data')
 for target,arch,fixture in [('darwin','arm64','graph_test.go'),('windows','amd64','windows_admission_test.go')]:
  overlay=tmp/(target+'.json');overlay.write_text(json.dumps({'Replace':{str(src/'src/wago/qualification_supplement_test.go'):str(rev/'fixtures'/fixture)}}))
  for mode in ['ordinary','wagodebug','wago_regalloccheck']:
   cmd=['/home/dev/.local/share/mise/installs/go/1.27.1/bin/go','test','-c','-overlay='+str(overlay),'-o',str(tmp/'test.bin'),* ([] if mode=='ordinary' else ['-tags='+mode]),'./src/wago']
   env=dict(os.environ,GOOS=target,GOARCH=arch,CGO_ENABLED='0',GOWORK='off',GOTOOLCHAIN='local',GOROOT='/home/dev/.local/share/mise/installs/go/1.27.1')
   result=subprocess.run(cmd,cwd=src,env=env,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT)
   (rev/(target+'-'+mode+'-overlay-build.log')).write_text(result.stdout)
   rows.append({'target':target,'mode':mode,'command':cmd,'exit':result.returncode,'execution':'NOT_RUN'})
   print(target,mode,result.returncode,flush=True)
 (rev/'overlay-builds.json').write_text(json.dumps({'source':spec['source'],'archive_sha256':hashlib.sha256(archive.read_bytes()).hexdigest(),'rows':rows},indent=2)+'\n')
assert all(x['exit']==0 for x in rows)
