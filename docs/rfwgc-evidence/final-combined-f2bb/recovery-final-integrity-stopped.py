"""Validate recorded provenance after all qualification commands have drained."""
import pathlib,json,hashlib
E=pathlib.Path(__file__).resolve().parent;M=json.loads((E/'source.json').read_text());W=pathlib.Path(M['scratch']);errors=[];rows=[]
def sha(p):return hashlib.sha256(pathlib.Path(p).read_bytes()).hexdigest()
def check(label,ok,**detail):
 rows.append(dict(check=label,passed=bool(ok),**detail))
 if not ok:errors.append(label)
for n,h in json.loads((E/'original-driver-hashes.json').read_text()).items():check('original-driver/'+n,sha(E/'original-drivers'/n)==h)
for n,r in json.loads((E/'drivers-integrity-after.json').read_text()).items():check('driver/'+n,r['unchanged'])
check('recorded-tools',json.loads((E/'tools-integrity-after.json').read_text())['unchanged'])
for n,r in json.loads((E/'recovery-extra-tools-before.json').read_text()).items():check('extra-tool/'+n,sha(n)==r['sha256'])
st=json.loads((E/'recovery-staticcheck-tool.json').read_text());check('staticcheck',sha(st['path'])==st['sha256'])
for r in json.loads((E/'exports-integrity-after.json').read_text()):check('export/'+r['export'],not r['changes'],changes=r['changes'])
for r in json.loads((E/'command-driver-integrity.json').read_text()):check('command-driver/'+r['command'],r['unchanged'])
for lane in ['qemu-collector','qemu-facade','qemu-guest','qemu-h6','qemu-rv64-controls','qemu-repairs']:
 d=E/lane;m=json.loads((d/'source.json').read_text());f=json.loads((d/'driver-final.json').read_text());driver=E/('target-repairs.py' if lane=='qemu-repairs' else lane+'.py')
 check(lane+'/source',m['pin']==M['pin'] and m['archive_sha256']==M['archive_sha256'])
 check(lane+'/driver',f['driver_unchanged'] and m['driver_sha256']==sha(driver))
 check(lane+'/emulator-package',m['package_sha256']==sha(m['package']))
 work=pathlib.Path(m['export']).parent
 for p in d.glob('*-binary.sha256'):
  label=p.name.removesuffix('-binary.sha256');b=work/(label+'.test');check(lane+'/'+label,sha(b)==p.read_text().strip())
  elf=json.loads((d/(label+'-elf.json')).read_text());check(lane+'/'+label+'/elf',int.from_bytes(b.read_bytes()[18:20],'little')==elf['machine'])
prov=(E/'tinygo-34/qualification-provenance.txt').read_text();check('tinygo-34/source','head='+M['pin']+'\n' in prov)
if (E/'tinygo-34/ci-fib-values.txt').exists():check('tinygo-34/fib-oracle',(E/'tinygo-34/ci-fib-values.txt').read_text().strip()=='PASS: all eleven fib outputs equal 6765')
else:rows.append(dict(check='tinygo-34/fib-oracle',status='NOT_RUN after blocking failure'))
for label in ['wago-tinygo','wago-tinygo-release']:
 p=W/'tinygo-source'/label;rows.append(dict(check='tinygo-binary/'+label,status='PRESENT' if p.is_file() else 'NOT_RUN after blocking failure',sha256=sha(p) if p.is_file() else None))
(E/'recovery-final-integrity.json').write_text(json.dumps(dict(source=M['pin'],passed=not errors,errors=errors,rows=rows),indent=2)+'\n')
print('integrity checks',len(rows),'failures',errors)
assert not errors
