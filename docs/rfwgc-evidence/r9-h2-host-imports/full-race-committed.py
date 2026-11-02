#!/usr/bin/env python3
import hashlib,json,os,pathlib,shutil,subprocess,tempfile
repo=pathlib.Path.cwd();e=repo/'docs/rfwgc-evidence/r9-h2-host-imports/combined-3b04e6837';p=json.loads((e/'integrated/provenance.json').read_text());src=pathlib.Path(p['source']);root=pathlib.Path(tempfile.mkdtemp(prefix='wago-h2-host-imports-full-race-'))
for n in ['cache','tmp']:(root/n).mkdir()
go='/home/dev/.local/share/mise/installs/go/1.27.3/bin/go';env=dict(os.environ,GOROOT=str(pathlib.Path(go).parent.parent),GOTOOLCHAIN='local',GOWORK='off',GOCACHE=str(root/'cache'),GOTMPDIR=str(root/'tmp'),WAGO_SPEC_INTERPRETER='/home/dev/Projects/wago/.tools/spec-interpreter-9d36019973201a19f9c9ebb0f10828b2fe2374aa/wasm',WAGO_SPEC_INTERPRETER_REVISION='9d36019973201a19f9c9ebb0f10828b2fe2374aa',WAGO_WAST2JSON='/home/dev/Projects/wago/.tools/wabt-1.0.41-linux-x64/bin/wast2json',WAGO_WABT_VERSION='1.0.41')
env['PATH']='/home/dev/Projects/wago/.tools/wabt-1.0.41-linux-x64/bin:/home/dev/.local/share/mise/installs/node/26.11.1/bin:'+str(pathlib.Path(go).parent)+':'+env['PATH']
for k in ['WAGO_UPDATE_STAGED_SPEC','WAGO_SPEC_FILES','WAGO_SPECTEST_DIR','WAGO_SPEC_VERSION']:env.pop(k,None)
sha=lambda p:hashlib.sha256(p.read_bytes()).hexdigest();before={str(f.relative_to(src)):sha(f) for f in src.rglob('*') if f.is_file()};rows=[]
try:
 for mode in ['ordinary','wagodebug','wago_regalloccheck']:
  cmd=[go,'test',*([] if mode=='ordinary' else ['-tags',mode]),'-race','./src/wago','-timeout=20m','-count=1','-v'];log=e/('full-race-'+mode+'.log')
  with log.open('w') as f:
   try:code=subprocess.run(cmd,cwd=src,env=env,stdout=f,stderr=subprocess.STDOUT,timeout=1500).returncode
   except subprocess.TimeoutExpired:code=124
  lines=log.read_text().splitlines();rows.append(dict(mode=mode,pin=p['pin'],command=cmd,exit=code,pass_count=sum(l.startswith('--- PASS:') for l in lines),skips=[l for l in lines if '--- SKIP:' in l],failures=[l for l in lines if '--- FAIL:' in l]))
  (e/'full-race-results.json').write_text(json.dumps(rows,indent=2)+'\n');print(mode,code,flush=True)
  if code:break
finally:
 changed=[f for f,h in before.items() if not (src/f).is_file() or sha(src/f)!=h];shutil.rmtree(root)
 (e/'full-race-final.json').write_text(json.dumps(dict(pin=p['pin'],all_pass=len(rows)==3 and all(r['exit']==0 for r in rows),export_changes=changed,removed=str(root)),indent=2)+'\n')
