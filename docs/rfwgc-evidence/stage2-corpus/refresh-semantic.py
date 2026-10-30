#!/usr/bin/env python3
# One preparation repair: opt-in must reach semantic correctness oracles too.
import pathlib,os,subprocess
out=pathlib.Path(__file__).resolve().parent
tmp=pathlib.Path((out/'temporary-directory.txt').read_text().strip())
go='/home/dev/.local/share/mise/installs/go/1.27.1/bin/go'
env=os.environ|{'GOROOT':str(pathlib.Path(go).parents[1]),'GOMAXPROCS':'1','GOTOOLCHAIN':'local','GOWORK':'off','GOFLAGS':'','GOEXPERIMENT':'','GOCACHE':str(tmp/'cache'),'GOTMPDIR':str(tmp)}
for name in ['baseline','stage2']:
 p=tmp/name/'bench/internal/semanticcorpus/run.go';s=p.read_text().replace('wago.Compile(nil, wasm)','wago.Compile(wago.NewRuntimeConfig().WithOptimization("gc-lifetime-analyze", os.Getenv("STAGE2_ON") == "1"), wasm)');p.write_text(s)
 for tag in ['', 'wagodebug','wago_regalloccheck']:
  suffix='-'+tag if tag else '';target=tmp/(name+suffix+'.test');new=tmp/(name+suffix+'.new.test')
  cmd=[go,'test','-p=1','-c','-o',str(new)]+(['-tags='+tag] if tag else [])+['./suite']
  with (out/(name+suffix+'-semantic-refresh.txt')).open('w') as f:subprocess.run(cmd,cwd=tmp/name/'bench',env=env,stdout=f,stderr=subprocess.STDOUT,check=True)
  new.replace(target)
