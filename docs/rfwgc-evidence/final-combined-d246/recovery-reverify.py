"""Read-only provenance census, before reuse; does not execute qualification."""
import pathlib,json,hashlib,gzip,subprocess,collections
E=pathlib.Path(__file__).resolve().parent; R=E.parents[2];M=json.loads((E/'source.json').read_text())
def sha(p):
 h=hashlib.sha256()
 with p.open('rb') as f:
  for b in iter(lambda:f.read(1048576),b''):h.update(b)
 return h.hexdigest()
p=subprocess.Popen(['git','archive',M['pin']],cwd=R,stdout=subprocess.PIPE);h=hashlib.sha256()
for b in iter(lambda:p.stdout.read(1048576),b''):h.update(b)
assert p.wait()==0 and h.hexdigest()==M['archive_sha256']
known={sha(E/n):n for n in ['run.py','run-v2.py','run-v3.py','run-v4.py']}; rows=[]
for p in sorted(E.glob('*/command.json')):
 c=json.loads(p.read_text());d=p.parent;rpath=d/'result.json';r=json.loads(rpath.read_text()) if rpath.exists() else {}
 raw=d/'raw.log';z=d/'raw.log.gz';hh=hashlib.sha256()
 if raw.exists() or z.exists():
  with (raw.open('rb') if raw.exists() else gzip.open(z,'rb')) as f:
   for b in iter(lambda:f.read(1048576),b''):hh.update(b)
 want=json.loads((E/'baseline.json').read_text())['pin'] if d.name.startswith(('baseline-','graphs-baseline-')) else M['pin']
 valid=bool(r and c.get('source')==r.get('source')==want and r.get('driver_unchanged') and c.get('driver_sha256') in known and hh.hexdigest()==r.get('raw_sha256'))
 rows.append(dict(suite=d.name,valid=valid,verdict=r.get('verdict','INTERRUPTED'),driver=known.get(c.get('driver_sha256')),raw_sha256=hh.hexdigest(),command_sha256=sha(p)))
processes=[]
# Exclude this inspection and its shell ancestors from ownership candidates.
import os
ancestors=set();pid=os.getpid()
while pid>1:
 ancestors.add(pid)
 try:pid=int(pathlib.Path('/proc',str(pid),'stat').read_text().rsplit(')',1)[1].split()[1])
 except OSError:break
for p in pathlib.Path('/proc').glob('[0-9]*'):
 try:
  cmd=(p/'cmdline').read_bytes().replace(b'\0',b' ').decode(errors='replace');cwd=str((p/'cwd').resolve())
  if ('wago-final-d246' in cmd or M['scratch'] in cwd or str(E) in cmd) and int(p.name) not in ancestors:processes.append(dict(pid=int(p.name),cmd=cmd,cwd=cwd))
 except (OSError,ProcessLookupError):pass
out=dict(source=M['pin'],archive_sha256=h.hexdigest(),receipts=rows,counts=dict(collections.Counter((r['verdict'] if r['valid'] else 'INTERRUPTED' if r['verdict']=='INTERRUPTED' else 'INVALID') for r in rows)),processes=processes)
(E/'recovery-reverification.json').write_text(json.dumps(out,indent=2)+'\n');print(json.dumps(out['counts']));print('processes',processes)
assert all(r['valid'] for r in rows if r['verdict']!='INTERRUPTED')
