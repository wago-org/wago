"""Seal completed nested-runner receipts; active logs are never sealed."""
import pathlib,json,hashlib,gzip,re
E=pathlib.Path(__file__).resolve().parent;M=json.loads((E/'source.json').read_text())
def raw(p):return p.read_bytes() if p.exists() else gzip.decompress(pathlib.Path(str(p)+'.gz').read_bytes())
def sha(p):return hashlib.sha256(raw(p)).hexdigest()
previous=json.loads((E/'nested-integrity.json').read_text()) if (E/'nested-integrity.json').exists() else {'files':{}}
for name,h in previous['files'].items():assert sha(E/name)==h, name
files=dict(previous['files']);rows=[]
def seal(p):files[str(p.relative_to(E))]=sha(p)
T=E/'tinygo-34'
if (T/'qualification-results.txt').exists():
 provenance=(T/'qualification-provenance.txt').read_text();assert 'head='+M['pin']+'\n' in provenance
 for p in [T/'qualification-provenance.txt',T/'qualification-bootstrap.log',E/'tinygo-34.sh']:seal(p)
 for line in (T/'qualification-results.txt').read_text().splitlines():
  name,rc=line.split(' exit=');rc=int(rc)
  for ext in ['.command','.log']:seal(T/(name+ext))
  rows.append({'suite':'tinygo-34/'+name,'exit':rc,'source':M['pin'],'command_sha256':sha(T/(name+'.command')),'raw_sha256':sha(T/(name+'.log'))})
for T in sorted(E.glob('qemu-*')):
 if not T.is_dir() or not (T/'results.json').exists():continue
 meta=json.loads((T/'source.json').read_text());assert meta['pin']==M['pin']
 source=pathlib.Path(meta['export']);archive=source.parent/'source.tar';assert sha(archive)==meta['archive_sha256']
 driver=E/('target-repairs.py' if T.name=='qemu-repairs' else T.name+'.py');assert sha(driver)==meta['driver_sha256']
 seal(T/'source.json');seal(driver)
 for row in json.loads((T/'results.json').read_text()):
  p=T/(row['label']+'.log');seal(p)
  rows.append({'suite':T.name+'/'+row['label'],'exit':row['exit'],'source':meta['pin'],'argv':row['command'],'raw_sha256':sha(p)})
 for p in T.glob('*-binary.sha256'):seal(p)
(E/'nested-integrity.json').write_text(json.dumps({'source':M['pin'],'files':files,'receipts':rows,'previous_seals_verified':True},indent=2)+'\n')
print('Nested terminal receipts sealed:',len(rows))
