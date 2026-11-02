#!/usr/bin/env python3
"""Build-only platform audit. No target execution or timing claims."""
import hashlib,json,os,pathlib,subprocess,tarfile,tempfile
root=pathlib.Path.cwd();out=root/'docs/rfwgc-evidence/final-review-runtime-repairs/nonhost-2a6dd04b1';out.mkdir(exist_ok=False)
pin=subprocess.check_output(['git','rev-parse','2a6dd04b1'],text=True).strip()
work=pathlib.Path(tempfile.mkdtemp(prefix='dr-nonhost-'));src=work/'source';src.mkdir()
archive=work/'source.tar'
with archive.open('wb') as f: subprocess.run(['git','archive',pin,'--','.',':!docs'],stdout=f,check=True)
with tarfile.open(archive) as f:f.extractall(src,filter='data')
go='/home/dev/.local/share/mise/installs/go/1.27.3/bin/go'
meta=dict(pin=pin,source=str(src),archive_sha256=hashlib.sha256(archive.read_bytes()).hexdigest(),driver_sha256=hashlib.sha256(pathlib.Path(__file__).read_bytes()).hexdigest(),execution='NOT_RUN: no Darwin or Windows runner')
(out/'provenance.json').write_text(json.dumps(meta,indent=2)+'\n');rows=[]
for target,arch in [('darwin','arm64'),('windows','amd64')]:
 env=dict(os.environ,GOOS=target,GOARCH=arch,CGO_ENABLED='0',GOWORK='off',GOROOT=str(pathlib.Path(go).parent.parent))
 selected=subprocess.check_output([go,'list','-f','{{.GoFiles}} {{.TestGoFiles}}','./src/wago'],cwd=src,env=env,text=True)
 (out/(target+'-selected-files.txt')).write_text(selected)
 for mode in ['ordinary','wagodebug','wago_regalloccheck']:
  label=target+'-'+arch+'-'+mode;cmd=[go,'test','-c','./src/wago','-o',str(work/(label+'.test'))]+([] if mode=='ordinary' else ['-tags',mode])
  with (out/(label+'.log')).open('w') as f:
   try: code=subprocess.run(cmd,cwd=src,env=env,stdout=f,stderr=subprocess.STDOUT,timeout=600).returncode
   except subprocess.TimeoutExpired:code=124
  rows.append(dict(label=label,command=cmd,exit=code,execution='NOT_RUN'))
  (out/'results.json').write_text(json.dumps(rows,indent=2)+'\n');print(label,code,flush=True)
raise SystemExit(0 if all(r['exit']==0 for r in rows) else 1)
