"""Private-copy checksum repair verification; never mutates the pinned export."""
import hashlib, json, os, pathlib, shutil, subprocess, tempfile
HERE=pathlib.Path(__file__).resolve().parent
ROOT=HERE.parents[3]
SOURCE='3242c18a19620aa77f39dd63cb5cd7544d4ca70a'
go=pathlib.Path('/home/dev/.local/share/mise/installs/go/1.27.1/bin/go')
rows=[]
def run(label,args,cwd,env):
 p=subprocess.run(args,cwd=cwd,env=env,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT)
 (HERE/(label+'.log')).write_text(p.stdout)
 rows.append(dict(label=label,argv=args,exit_code=p.returncode))
 (HERE/'installer-checks.json').write_text(json.dumps(rows,indent=2)+'\n')
 return p
with tempfile.TemporaryDirectory(prefix='installer-check-') as tmp:
 t=pathlib.Path(tmp); m=t/'module'; m.mkdir()
 for name in ['go.mod','main.go','main_test.go']:
  (m/name).write_bytes(subprocess.check_output(['git','show',SOURCE+':cli/wago-installer/'+name],cwd=ROOT))
 env=dict(os.environ,GOROOT=str(go.parent.parent),PATH=str(go.parent)+':/usr/bin:/bin',GOTOOLCHAIN='local',GOWORK='off',GOENV='off',GOPROXY='https://proxy.golang.org',GOSUMDB='sum.golang.org',GOPRIVATE='',GONOSUMDB='',GONOPROXY='',GOMODCACHE=str(t/'cache'))
 p=run('installer-before',[str(go),'test','-mod=readonly','./...'],m,env)
 assert p.returncode and 'missing go.sum entry' in p.stdout
 assert run('installer-download',[str(go),'mod','download','-json','github.com/wago-org/wago','golang.org/x/sys'],m,env).returncode==0
 sums=(m/'go.sum').read_bytes(); mod=(m/'go.mod').read_bytes()
 for mode in ['ordinary','wagodebug','wago_regalloccheck']:
  assert run('installer-'+mode,[str(go),'test','-mod=readonly',* ([] if mode=='ordinary' else ['-tags='+mode]),'-count=1','./...'],m,env).returncode==0
 assert run('installer-verify',[str(go),'mod','verify'],m,env).returncode==0
 assert sums==(m/'go.sum').read_bytes() and mod==(m/'go.mod').read_bytes()
 # Store the authenticated checksum database lookup, not just a locally cached sum.
 for p in (t/'cache/cache/download/sumdb').rglob('*'):
  if p.is_file() and '/lookup/' in str(p):
   (HERE/('sumdb-'+p.name.replace('.','_')+'.txt')).write_bytes(p.read_bytes())
 (HERE/'verified-go.sum').write_bytes(sums)
