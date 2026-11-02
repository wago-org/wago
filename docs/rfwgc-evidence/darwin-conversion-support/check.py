#!/usr/bin/env python3
"""Correctness checks only; immutable exports may run this from their root."""
import json, os, pathlib, subprocess, sys
out=pathlib.Path(sys.argv[1]); out.mkdir(parents=True,exist_ok=True)
go='/home/dev/.local/share/mise/installs/go/1.27.1/bin/go'
env=dict(os.environ,GOROOT=str(pathlib.Path(go).parent.parent),GOWORK='off',GOMAXPROCS='4')
env['PATH']=str(pathlib.Path(go).parent)+':'+env['PATH']
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
