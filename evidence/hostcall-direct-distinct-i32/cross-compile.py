#!/usr/bin/env python3
"""Compile affected test packages only; never claim foreign execution."""
import json, os, shutil, subprocess, tempfile
from pathlib import Path
root=Path(__file__).resolve().parents[2]
out=Path(__file__).resolve().parent
results=[]
for goos,goarch in [('linux','arm64'),('linux','riscv64'),('darwin','amd64'),('darwin','arm64'),('windows','amd64'),('windows','arm64')]:
    for package in ['./src/core/runtime','./src/wago']:
        name='cross-'+goos+'-'+goarch+'-'+package.rsplit('/',1)[1]
        tmp=tempfile.mkdtemp(prefix='wago-direct-cross-')
        command=['go','test','-p=2','-c','-o',tmp+'/candidate.test',package]
        env=dict(os.environ,GOOS=goos,GOARCH=goarch,CGO_ENABLED='0',GOTMPDIR=tmp)
        print('START',name,flush=True)
        try:
            with (out/(name+'.log')).open('w') as log:
                result=subprocess.run(command,cwd=root,env=env,stdout=log,stderr=subprocess.STDOUT)
            results.append(dict(name=name,command=command,environment={k:env[k] for k in ['GOOS','GOARCH','CGO_ENABLED']},head=subprocess.check_output(['git','rev-parse','HEAD'],cwd=root,text=True).strip(),exit_code=result.returncode,executed=False))
            (out/'cross.json').write_text(json.dumps(results,indent=2)+'\n')
            print('END',name,result.returncode,flush=True)
        finally:
            shutil.rmtree(tmp)

raise SystemExit(1 if any(result['exit_code'] != 0 for result in results) else 0)
