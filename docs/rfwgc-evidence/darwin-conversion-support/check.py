#!/usr/bin/env python3
"""Correctness checks only; immutable exports may run this from their root."""
import json, os, pathlib, subprocess, sys
out=pathlib.Path(sys.argv[1]); out.mkdir(parents=True,exist_ok=True)
go='/home/dev/.local/share/mise/installs/go/1.27.1/bin/go'
env=dict(os.environ,GOROOT=str(pathlib.Path(go).parent.parent),GOWORK='off',GOMAXPROCS='4')
tool_paths=[str(pathlib.Path(go).parent), '/home/dev/Projects/wago/.tools/wabt-1.0.41-linux-x64/bin', '/home/dev/.local/share/mise/installs/github-bytecodealliance-wasm-tools/1.251.0', '/home/dev/.local/share/mise/installs/node/24.22.2/bin']
env['PATH']=':'.join(tool_paths)+':'+env['PATH']
env.update(WAGO_WAST2JSON='/home/dev/Projects/wago/.tools/wabt-1.0.41-linux-x64/bin/wast2json',WAGO_WABT_VERSION='1.0.41',WAGO_SPEC_INTERPRETER='/home/dev/Projects/wago/.tools/spec-interpreter-9d36019973201a19f9c9ebb0f10828b2fe2374aa/wasm',WAGO_SPEC_INTERPRETER_REVISION='9d36019973201a19f9c9ebb0f10828b2fe2374aa')
for key in ['WAGO_UPDATE_STAGED_SPEC','WAGO_SPEC_FILES','WAGO_SPECTEST_DIR','WAGO_SPEC_VERSION']:
 env.pop(key,None)
(out/'environment.json').write_text(json.dumps({k:env.get(k) for k in ['PATH','GOROOT','GOWORK','GOMAXPROCS','WAGO_BOUNDS','WAGO_WAST2JSON','WAGO_WABT_VERSION','WAGO_SPEC_INTERPRETER','WAGO_SPEC_INTERPRETER_REVISION']},indent=2)+'\n')
regex='^(TestConverted.*|TestExternrefGlobalHoldingConvertedGCReference|TestR9H2.*|TestR9Ownership.*|TestManagedFunctionHostShape)$'
rows=[]
for mode in ['ordinary','wagodebug','wago_regalloccheck']:
 tags=[] if mode=='ordinary' else ['-tags='+mode]
 suites=[('focused',['./src/wago','-run',regex,'-timeout=3m'])]
 if '--full' in sys.argv:
  suites += [('full',['./src/wago','-timeout=20m']),('collector',['./src/core/runtime/gc/native','-timeout=10m']),('backend',['./src/core/compiler/backend/railshot/amd64','./src/core/compiler/backend/railshot/arm64','./src/core/compiler/wasm','-timeout=10m']),('race',['-race','./src/wago','-run',regex,'-timeout=5m'])]
 for suite,args in suites:
  name=mode+'-'+suite; cmd=[go,'test',*tags,*args,'-count=1','-v']
  with (out/(name+'.log')).open('w') as f:
   try: code=subprocess.run(cmd,env=env,stdout=f,stderr=subprocess.STDOUT,timeout=1500).returncode
   except subprocess.TimeoutExpired: code=124
  row=dict(name=name,command=cmd,exit=code);rows.append(row)
  (out/'results.json').write_text(json.dumps(rows,indent=2)+'\n');print(name,code,flush=True)
