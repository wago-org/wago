#!/usr/bin/env python3
"""Cross-build declared Darwin broad-tag overlays; never executes a foreign binary."""
import argparse, json, os, pathlib, subprocess
import contracts
p=argparse.ArgumentParser(description=__doc__);p.add_argument('source',type=pathlib.Path);p.add_argument('output',type=pathlib.Path);a=p.parse_args();a.output.mkdir(parents=True,exist_ok=False)
overlay=a.output/'overlay.json';overlay.write_text(json.dumps({'Replace':contracts.darwin_overlays(a.source)},indent=2)+'\n')
go=pathlib.Path(os.environ['GOROOT'])/'bin/go';env=dict(os.environ,GOOS='darwin',GOARCH='arm64',GOWORK='off',GOTOOLCHAIN='local',CGO_ENABLED='0');rows=[]
for mode in ['ordinary','wagodebug','wago_regalloccheck']:
 for variant in ['wago_guardpage','wago_profile','wago_codegenstats']:
  label=mode+'-'+variant;tags=variant+('' if mode=='ordinary' else ','+mode)
  args=[str(go),'test','-c','-mod=readonly','-overlay='+str(overlay),'-tags='+tags,'-o',str(a.output/(label+'.test')),'./src/wago']
  with (a.output/(label+'.log')).open('w') as log:result=subprocess.run(args,cwd=a.source,env=env,stdout=log,stderr=subprocess.STDOUT)
  rows.append(dict(argv=args,exit_code=result.returncode));(a.output/'commands.json').write_text(json.dumps(rows,indent=2)+'\n')
  assert result.returncode==0,label
  (a.output/(label+'.test')).unlink()
(a.output/'result.json').write_text(json.dumps({'status':'PASS','source':contracts.SPEC['source'],'native_execution':'NOT_RUN','cross_builds':9},indent=2)+'\n')
