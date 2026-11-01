#!/usr/bin/env python3
import hashlib,json,pathlib,re,subprocess
root=pathlib.Path.cwd();out=root/'docs/rfwgc-evidence/stage6-parallel'
rows=[]
for path in sorted(out.rglob('*.txt')):
 if path.name.endswith('.go.txt'):continue
 text=path.read_text()
 rows.append({'log':str(path.relative_to(out)),'top_level_passes':len(re.findall(r'^--- PASS:',text,re.M)),'failures':re.findall(r'^--- FAIL:.*',text,re.M),'skips':re.findall(r'^--- SKIP:.*',text,re.M),'race_warning':'WARNING: DATA RACE' in text})
(out/'summary.json').write_text(json.dumps(rows,indent=2)+'\n')
owned=subprocess.check_output(['git','diff-tree','--no-commit-id','--name-only','-r','bc05eef3e','--','src/core/runtime/gc/native'],text=True).splitlines()
owned=[p for p in owned if pathlib.Path(p).exists()]
for p in ['parallel_callback_integration_linux_test.go','parallel_callbacks_linux_test.go','parallel_disabled_test.go']:
 owned.append('src/core/runtime/gc/native/'+p)
(out/'final-owned-source.json').write_text(json.dumps({'head':subprocess.check_output(['git','rev-parse','HEAD'],text=True).strip(),'files':{p:hashlib.sha256(pathlib.Path(p).read_bytes()).hexdigest() for p in sorted(set(owned))}},indent=2)+'\n')
