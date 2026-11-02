import pathlib,subprocess,os,json,tarfile,hashlib
out=pathlib.Path('/tmp/pr1048-windows-capability-repair');src=out/'source';src.mkdir()
pin=subprocess.check_output(['git','rev-parse','HEAD'],text=True).strip()
with (out/'source.tar').open('wb') as f:subprocess.run(['git','archive',pin],stdout=f,check=True)
with tarfile.open(out/'source.tar') as t:t.extractall(src,filter='data')
go='/home/dev/.local/share/mise/installs/go/1.27.1/bin/go';env=dict(os.environ,GOROOT=str(pathlib.Path(go).parent.parent),GOTOOLCHAIN='local',GOPROXY='off',GOWORK='off')
manifest={str(p.relative_to(src)):hashlib.sha256(p.read_bytes()).hexdigest() for p in src.rglob('*') if p.is_file()}
(out/'provenance.json').write_text(json.dumps({'pin':pin,'go':go,'source_files':manifest},indent=2))
rows=[]
def run(label,cmd,e):
 with (out/(label+'.log')).open('w') as f:r=subprocess.run(cmd,cwd=src,env=e,stdout=f,stderr=subprocess.STDOUT,timeout=600)
 rows.append({'label':label,'argv':cmd,'exit':r.returncode});(out/'results.json').write_text(json.dumps(rows,indent=2));print(label,r.returncode,flush=True)
 if r.returncode:raise SystemExit(1)
for mode in ['ordinary','wagodebug','wago_regalloccheck']:
 tags=[] if mode=='ordinary' else ['-tags='+mode]
 run(mode+'-host',[go,'test',*tags,'./src/wago','-run','^Test(DesignRepairPublicGlobalTokenClassification|DefaultCoreFeaturePolicy|CoreFeature.*)$','-count=1','-v'],env)
 ft='wago_target_tinygo'+('' if mode=='ordinary' else ','+mode)
 run(mode+'-fallback',[go,'test','-tags='+ft,'./src/wago','-run','^Test(UnsupportedConversion|DesignRepairUnsupportedConvertedPublicToken|DesignRepairPublicGlobalTokenClassification)','-count=1','-v'],env)
 for system,arch in [('windows','amd64'),('darwin','arm64')]:
  run(mode+'-'+system,[go,'test',*tags,'./src/wago','-run','^$','-exec=true'],dict(env,GOOS=system,GOARCH=arch))
changes=[rel for rel,h in manifest.items() if hashlib.sha256((src/rel).read_bytes()).hexdigest()!=h]
(out/'integrity.json').write_text(json.dumps({'source_changes':changes,'commands':len(rows),'all_pass':not changes}));assert not changes
