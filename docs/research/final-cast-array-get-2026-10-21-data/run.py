#!/usr/bin/env python3
import json,os,subprocess,sys,time
from pathlib import Path
name=sys.argv[1];cmd=sys.argv[2:];root=Path(__file__).resolve().parent
start=time.monotonic()
with (root/(name+'.log')).open('w') as out:
 p=subprocess.run(cmd,stdout=out,stderr=subprocess.STDOUT)
r={'command':cmd,'cwd':os.getcwd(),'seconds':time.monotonic()-start,'exit_code':p.returncode,'GOMAXPROCS':os.getenv('GOMAXPROCS'),'GOWORK':os.getenv('GOWORK')}
(root/(name+'.json')).write_text(json.dumps(r,indent=2)+'\n');print(json.dumps(r),flush=True)
sys.exit(p.returncode)
