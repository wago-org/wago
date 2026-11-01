#!/usr/bin/env python3
"""Opt-in state assertions only. Archives must come from the specified pins."""
import hashlib,json,pathlib,subprocess
root=pathlib.Path(__file__).resolve().parent
go='/home/dev/.local/share/mise/installs/go/1.27.3/bin/go'
probe=root/'testdata/r9_converted_provider_state_test.go'
results=[]
for label,pin in [('main','90f2b57dafa7f8c3cc7e3aca38b91148f15e1d1e'),('port-updated','45c83820fbc10ee31eaaa8437e12464955b2fb9d')]:
 archive=root/'snapshots'/label
 (archive/'src/wago'/probe.name).write_bytes(probe.read_bytes())
 for mode in ['ordinary','wagodebug','wago_regalloccheck']:
  cmd=[go,'test','./src/wago','-run','^TestR9ConvertedProviderState$','-count=1','-v']
  if mode!='ordinary':cmd+=['-tags',mode]
  with (root/f'{label}-{mode}.log').open('w') as out:
   result=subprocess.run(cmd,cwd=archive,stdout=out,stderr=subprocess.STDOUT,timeout=600)
  results.append(dict(label=label,pin=pin,mode=mode,command=cmd,exit_code=result.returncode,probe_sha256=hashlib.sha256(probe.read_bytes()).hexdigest()))
  (root/'results.json').write_text(json.dumps(results,indent=2)+'\n')
  print(label,mode,result.returncode,flush=True)
