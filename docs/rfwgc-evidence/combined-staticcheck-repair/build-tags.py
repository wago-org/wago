#!/usr/bin/env python3
import sys,importlib.util,json
sys.argv=['qualify.py','support']
spec=importlib.util.spec_from_file_location('qualification',__file__.replace('build-tags.py','qualify.py'));q=importlib.util.module_from_spec(spec);spec.loader.exec_module(q)
checks=[]
for osname,arch in [('linux','amd64'),('linux','arm64'),('linux','riscv64'),('linux','386'),('windows','amd64'),('darwin','arm64')]:
 for tags in ['','tinygo','wago_target_tinygo']:
  label='selection-'+osname+'-'+arch+'-'+(tags or 'ordinary')
  code=q.run(label,[q.G+'/bin/go','list','-json',*(['-tags='+tags] if tags else []),'./src/wago'],dict(GOOS=osname,GOARCH=arch,CGO_ENABLED='0'))
  assert code==0
  files=json.loads((q.O/label/'raw.log').read_text())['GoFiles']
  shape=[p for p in files if p.startswith('function_environment_shape_')]
  expected=[] if tags or osname!='linux' else ['function_environment_shape_'+('riscv64' if arch=='riscv64' else 'host')+'.go']
  assert shape==expected,(label,shape,expected)
  assert ('function_environment_linux.go' in files)==bool(expected)
  assert ('function_environment_mark_linux.go' in files)==bool(expected)
  checks.append(dict(label=label,target_shapes=shape,pass_selection=True))
(q.E/'build-selection.json').write_text(json.dumps(checks,indent=2)+'\n')
