#!/usr/bin/env python3
import json, os, pathlib, subprocess, sys, time
root=pathlib.Path(__file__).resolve().parents[3]
name=sys.argv[1]; cmd=sys.argv[2:]; out=pathlib.Path(__file__).parent
cwd=root
if cmd and cmd[0].startswith('--cwd='): cwd=root/cmd.pop(0)[6:]
env=os.environ.copy(); env.pop('GOWORK',None); env.update(GOMAXPROCS='2',CGO_ENABLED='0',DOTNET_SYSTEM_GLOBALIZATION_INVARIANT='1')
t=time.monotonic()
with (out/(name+'.log')).open('w') as f: p=subprocess.run(cmd,cwd=cwd,env=env,stdout=f,stderr=subprocess.STDOUT)
r={'command':cmd,'cwd':str(cwd),'seconds':time.monotonic()-t,'exit':p.returncode};(out/(name+'.json')).write_text(json.dumps(r,indent=2)+'\n'); print(json.dumps(r));print((out/(name+'.log')).read_text()[-12000:]);sys.exit(p.returncode)
