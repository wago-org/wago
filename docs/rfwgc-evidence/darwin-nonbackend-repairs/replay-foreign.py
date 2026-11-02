#!/usr/bin/env python3
"""Replay only the WABT-blocked foreign fallback, retaining original failures."""
import hashlib,json,os,pathlib,subprocess
out=pathlib.Path(__file__).resolve().parent
q=json.loads((out/'qualification.json').read_text())
wabt='/home/dev/.local/share/mise/installs/github-web-assembly-wabt/1.0.42/bin/wat2wasm'
env=os.environ.copy()
for k in list(env):
 if k.startswith('WAGO_'):env.pop(k)
env.update(PATH=str(pathlib.Path(wabt).parent)+os.pathsep+env['PATH'],GOMAXPROCS='4')
report={'source':q['source'],'wat2wasm':wabt,'wat2wasm_sha256':hashlib.sha256(pathlib.Path(wabt).read_bytes()).hexdigest(),'cells':[]}
for c in q['cells']:
 if c['target']!='linux-arm64' or c.get('build_exit')!=0:continue
 binary=c['build_command'][4]
 assert hashlib.sha256(pathlib.Path(binary).read_bytes()).hexdigest()==c['binary_sha256']
 cmd=[q['qemu'],binary,'-test.v','-test.count=1','-test.timeout=180s','-test.run=^TestSharedEntryGuardForeignFallback$']
 with (out/('linux-arm64-'+c['mode']+'-foreign-replay.log')).open('w') as f:
  rc=subprocess.run(cmd,cwd=pathlib.Path(q['export'])/'src/wago',env=env,stdout=f,stderr=subprocess.STDOUT).returncode
 report['cells'].append({'mode':c['mode'],'command':cmd,'binary_sha256':c['binary_sha256'],'execution_exit':rc})
(out/'foreign-replay.json').write_text(json.dumps(report,indent=2)+'\n')
