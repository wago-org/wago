#!/usr/bin/env python3
"""Same final tests/fixtures, exact common production baseline; no runtime knob."""
import hashlib,json,pathlib,subprocess
root=pathlib.Path(__file__).resolve().parents[3]
base='c6db3230a'
base=subprocess.check_output(['git','rev-parse',base],cwd=root,text=True).strip()
out=pathlib.Path(__file__).parent/'control';out.mkdir(exist_ok=True)
paths=['call.go','compile.go','gc_struct.go','gc_trapping_guard.go']
prefix='src/core/compiler/backend/railshot/amd64/'
replace={};sources={}
for name in paths:
 rel=prefix+name;src=root/rel
 data=(b'//go:build amd64\n\npackage amd64\n' if name=='gc_trapping_guard.go' else subprocess.check_output(['git','show',base+':'+rel],cwd=root))
 target=out/(name+'.txt');target.write_bytes(data);replace[str(src)]=str(target)
 sources[rel]={'candidate_sha256':hashlib.sha256(src.read_bytes()).hexdigest(),'control_sha256':hashlib.sha256(data).hexdigest()}
info={'common_base':base,'production':sources,'fixture_sha256':hashlib.sha256((root/'tests/fixtures/wasm/gc-trapping-guard.wasm').read_bytes()).hexdigest()}
info['test_sources']={str(p.relative_to(root)):hashlib.sha256(p.read_bytes()).hexdigest() for p in [root/'src/wago/gc_trapping_guard_amd64_test.go',root/'src/wago/gc_trapping_guard_stats_amd64_test.go',*sorted((root/'tests/fixtures/wasm').glob('gc-trapping-guard*'))]}
(pathlib.Path(__file__).parent/'control-overlay.json').write_text(json.dumps({'Replace':replace},indent=2)+'\n')
(pathlib.Path(__file__).parent/'source.json').write_text(json.dumps(info,indent=2)+'\n')
print(json.dumps(info,indent=2))
