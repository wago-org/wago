#!/usr/bin/env python3
"""Supply pinned fixture archives, then run unchanged host qualification commands."""
import sys,importlib.util,json,subprocess,tarfile,pathlib,hashlib
sys.argv=['qualify.py','host-replay']
spec=importlib.util.spec_from_file_location('qualification',__file__.replace('host-replay.py','qualify.py'));q=importlib.util.module_from_spec(spec);spec.loader.exec_module(q)
revision=json.loads((q.E/'source.json').read_text())['pin'];rows=[]
for v in ['v1','v2','v3']:
 path='tests/conformance/spec-'+v
 line=subprocess.check_output(['git','ls-tree',revision,path],text=True,cwd=q.E.parents[2]);pin=line.split()[2]
 archive=q.W/('spec-'+v+'.tar');repo=q.E.parents[2]/path
 with archive.open('wb') as f:subprocess.run(['git','-C',str(repo),'archive',pin],stdout=f,check=True)
 with tarfile.open(archive) as f:f.extractall(q.S/path,filter='data')
 rows.append(dict(path=path,pin=pin,sha256=q.sha(archive)))
(q.E/'fixture-archives.json').write_text(json.dumps(rows,indent=2)+'\n')
wabt='/home/dev/Projects/wago/.tools/wabt-1.0.41-linux-x64/bin'
interpreter='/home/dev/Projects/wago/.tools/spec-interpreter-9d36019973201a19f9c9ebb0f10828b2fe2374aa/wasm'
q.env.update(PATH=wabt+':'+q.env['PATH'],WAGO_SPEC_INTERPRETER=interpreter,WAGO_SPEC_INTERPRETER_REVISION='9d36019973201a19f9c9ebb0f10828b2fe2374aa',WAGO_WAST2JSON=wabt+'/wast2json',WAGO_WABT_VERSION='1.0.41')
(q.E/'host-tool-environment.json').write_text(json.dumps({k:v for k,v in q.env.items() if k.startswith('WAGO_') or k=='PATH'},indent=2)+'\n')
(q.E/'host-tools.json').write_text(json.dumps({p:q.sha(p) for p in [q.G+'/bin/go',wabt+'/wast2json',interpreter]},indent=2)+'\n')
for mode in ['ordinary','wagodebug','wago_regalloccheck']:
 tags=[] if mode=='ordinary' else ['-tags='+mode]
 args=[q.G+'/bin/go','test','-json','-count=1','-timeout=35m','-failfast',*tags]
 if q.run('packages-fixtures-'+mode,args+['./src/core/runtime/gc/...','./src/core/runtime','./src/wago']):sys.exit(1)
 if q.run('ownership-race-'+mode,args+['-race','-run=^(TestR9H2.*|TestR9Ownership.*|TestGCExternConversion.*|TestGCConversion.*|TestPreparedGCRevocable.*|TestManagedFunction.*)$','./src/wago']):sys.exit(1)
