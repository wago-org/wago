#!/usr/bin/env python3
"""Offline evidence validation; never executes tests or measures performance."""
import gzip,hashlib,json,re,subprocess
from pathlib import Path
E=Path(__file__).resolve().parent
pin='d6f4e94fe671542d7e22fe84e8fe1f9358661c3a'
manifest=json.loads(gzip.decompress((E/'source-manifest.json.gz').read_bytes()))
tree={line.split('\t')[1]:line.split()[2] for line in subprocess.check_output(['git','ls-tree','--full-tree','-r',pin],cwd=E,text=True).splitlines() if line.split()[1]=='blob'}
assert len(tree)==len(manifest)==18604
assert all(tree[m['path']]==m['git'] for m in manifest)
commands=[]
for f in ['commands.json','retry-commands.json','supplement-commands.json']:
 receipt=json.loads((E/f).read_text());assert receipt['source']==pin;commands+=receipt['commands']
byname={c['name']:c for c in commands}
census={}
for mode in ['ordinary','wagodebug','wago_regalloccheck']:
 for suffix,prefix,count in [('focused','',53),('diagnostics','final-',62),('public','final-',12),('worker-diagnostics','',8)]:
  name=prefix+mode+'-'+suffix;c=byname[name];assert c['exit']==0
  text=(E/c['log']).read_text()
  counts={s:len(re.findall(r'^\s*--- '+s+':',text,re.M)) for s in ['PASS','FAIL','SKIP']}
  assert counts=={'PASS':count,'FAIL':0,'SKIP':0},(name,counts)
  assert text.rstrip().endswith('PASS');census[name]=counts
 for label in ['backend','public','diagnostics']:
  assert byname[mode+'-darwin-'+label+'-build']['exit']==0
assert byname['negative-build']['exit']==0 and byname['negative']['exit']==1
assert 'native guard target 0x444 does not start with STP FP,LR' in (E/byname['negative']['log']).read_text()
for line in (E/'SHA256SUMS').read_text().splitlines():
 expected,name=line.split('  ',1);assert hashlib.sha256((E/name).read_bytes()).hexdigest()==expected,name
print(json.dumps({'source':pin,'census':census,'darwin_cross_builds':9,'negative_control':'expected FAIL','verification':'PASS'},indent=2))
