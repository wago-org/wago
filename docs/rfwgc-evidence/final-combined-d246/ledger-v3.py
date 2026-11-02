"""Exact-pin terminal ledger; old scope contributes names, never verdicts."""
import pathlib,json,gzip,hashlib,collections
E=pathlib.Path(__file__).resolve().parent
M=json.loads((E/'source.json').read_text());pin=M['pin'];base=json.loads((E/'baseline.json').read_text())['pin']
def read(p):
 try:return json.loads(p.read_text() if p.exists() else gzip.open(str(p)+'.gz','rt').read())
 except (OSError,ValueError,EOFError):return {}
def raw(d):
 p=d/'raw.log'
 if p.exists():return p.read_bytes()
 p=d/'raw.log.gz'
 return gzip.decompress(p.read_bytes()) if p.exists() and p.stat().st_size else b''
Q=read(E/'qualification.json');rows=Q['rows'];by={r['suite']:r for r in rows};attempts=[];skips=[]
known={hashlib.sha256((E/n).read_bytes()).hexdigest():n for n in ['run.py','run-v2.py','run-v3.py']}
for p in sorted(E.glob('*/command.json')):
 c=read(p);r=read(p.parent/'result.json');b=raw(p.parent);n=p.parent.name
 want=base if n.startswith(('baseline-','graphs-baseline-')) else pin
 valid=bool(r and r.get('source')==c.get('source')==want and r.get('driver_unchanged') and c.get('driver_sha256') in known and r.get('raw_sha256')==hashlib.sha256(b).hexdigest())
 attempts.append(dict(suite=n,source=c.get('source'),receipt_valid=valid,driver=known.get(c.get('driver_sha256')),exit=r.get('exit'),verdict=('PASS' if r['exit']==0 else 'FAIL') if valid else 'INTERRUPTED' if not r else 'INVALID_RECEIPT'))
 if n in by and not valid:by[n]['verdict']=attempts[-1]['verdict']
 if n.startswith('resume-') and valid:by[n.removeprefix('resume-')]=by[n]
 if n.startswith('corrected-') and valid:by[n.removeprefix('corrected-')]=by[n]
 if n.startswith('tmp-resume-') and valid:by[n.removeprefix('tmp-resume-')]=by[n]
 for x in by.get(n,{}).get('skips',[]):skips.append(dict(suite=n,kind='go-json',detail=x))
 for x in by.get(n,{}).get('plain_skips',[]):skips.append(dict(suite=n,kind='plain',detail=x))
# Same recipe and explicit tool path, without forcing a redundant replay.
if 'replay-native-compare' not in by and 'tooling-native-compare' in by:by['replay-native-compare']=by['tooling-native-compare']
for lane in ['qemu-repairs']:
 for r in read(E/lane/'results.json') or []:
  name=lane+'/'+r['label'];by[name]=dict(suite=name,verdict='PASS' if r['exit']==0 else 'FAIL',source=read(E/lane/'source.json').get('pin'),exit=r['exit'],skips=r['skip'])
  if by[name]['source']!=pin:by[name]['verdict']='INVALID_RECEIPT'
for lane in ['qemu-collector','qemu-facade','qemu-guest','qemu-h6','qemu-rv64-controls','qemu-repairs']:
 for r in read(E/lane/'results.json') or []:
  for s in r['skip']:skips.append(dict(suite=lane+'/'+r['label'],kind='target',detail=s))
required=read(E/'required-gates.json');effective=[]
for n in required['prior_gate_names_only']+required['additional']:
 r=by.get(n,{})
 effective.append(dict(suite=n,receipt_suite=r.get('suite'),source=r.get('source'),verdict=r.get('verdict','NOT_RUN')))
counts=dict(collections.Counter(r['verdict'] for r in effective))
(E/'final-summary.json').write_text(json.dumps(dict(source=pin,baseline=base,verdict='FAILED / INCOMPLETE' if any(r['verdict']=='FAIL' for r in effective) else 'PASS' if all(r['verdict']=='PASS' for r in effective) else 'INCOMPLETE',required=effective,counts=counts,attempts=attempts,remote_targets='Darwin/Windows verdict owned by parent; no local execution promotion'),indent=2)+'\n')
# Retain surrounding plain-runner reasons, not only terminal SKIP lines.
for lane in ['tinygo-34','qemu-collector','qemu-facade','qemu-guest','qemu-h6','qemu-rv64-controls','qemu-repairs']:
 for log in sorted((E/lane).glob('*.log')):
  ls=log.read_text(errors='replace').splitlines()
  for i,l in enumerate(ls):
   if '--- SKIP:' in l:
    suite=lane+'/'+log.stem;detail='\n'.join(ls[max(0,i-4):i+1])
    existing=next((x for x in skips if x['suite']==suite and x['kind']=='target' and x['detail']==l),None)
    if existing:existing.update(kind='target-with-reason',detail=detail)
    else:skips.append(dict(suite=suite,kind='plain-with-reason',detail=detail))
# TinyGo boundary helpers return before the body but are printed as PASS.
import re
for log in sorted((E/'tinygo-34').glob('*.log')):
 current=None;ls=log.read_text(errors='replace').splitlines()
 for i,line in enumerate(ls):
  m=re.match(r'^\s*=== RUN\s+(.+)$',line)
  if m:current=m.group(1).strip()
  if any(reason in line for reason in ['test depends on standard Go testing control flow or struct layout','wat2wasm-backed test is unavailable under TinyGo']):
   skips.append(dict(suite='tinygo-34/'+log.stem,kind='excluded-body-despite-terminal-PASS',test=current,reason=line.strip(),detail='\n'.join(ls[max(0,i-1):i+3]),body_execution_claim=False))
(E/'exact-skip-census.json').write_text(json.dumps(skips,indent=2)+'\n')
print('required',len(effective),counts,'captured skips',len(skips))
