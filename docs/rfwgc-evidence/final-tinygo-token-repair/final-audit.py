import pathlib,json,hashlib,subprocess
E=pathlib.Path(__file__).resolve().parent; M=json.loads((E/'source.json').read_text());W=pathlib.Path(M['workspace'])
def sha(p):return hashlib.sha256(pathlib.Path(p).read_bytes()).hexdigest()
c=json.loads((E/'full34-census.json').read_text());assert c['passed']==34 and not c['unrun']
for row in c['gates']:
 if row['gate'].startswith('host-'):
  for name in ['TestR9H2HostContainerPublicGlobal','TestR9H2HostImportGlobal','TestR9H2HostImportGlobalOrdinaryAndGuards']:assert name in row['events']['PASS'],(row['gate'],name)
s=json.loads((E/'supplement-results.json').read_text());r=json.loads((E/'resume-results.json').read_text());x=json.loads((E/'native-converted-results.json').read_text())
assert len(s)==13 and s[-1]['label']=='native-amd64-ordinary' and s[-1]['exit']==1
for row in s[:12]:
 assert row['exit']==0 and not row['skipped'] and not row['failed'] and not row['missing']
 label=row['label']; expected=30 if label=='tinygo-evacuation-wagodebug' else 15 if 'evacuation' in label else 9 if 'joint' in label else 5 if 'conversion' in label else 22
 assert len(row['passed'])==expected,(label,len(row['passed']))
assert len(r)==18 and len(x)==6
for row in r+x:
 assert row['exit']==0 and not row['skipped'] and not row['failed'] and not row['missing'],row['label']
 label=row['label'];expected=0 if label.endswith('-build') else 13 if 'tinygo-regression' in label else 7 if label.startswith('converted-arm64') else 5 if label.startswith('converted-riscv64') else 24 if 'riscv64' in label else 40
 assert len(row['passed'])==expected,(label,len(row['passed']))
assert not (E/'SAFETY-STOP.json').exists()
for name in ['supplement','resume']:
 meta=json.loads((E/(name+'-provenance.json')).read_text()); driver='supplements.py' if name=='supplement' else 'resume.py';assert sha(E/driver)==meta['driver_sha256']
assert sha(E.parents[2]/'src/wago/design_repair_public_global_token_test.go')==json.loads((E/'resume-provenance.json').read_text())['regression_sha256']
records=[]
for x in subprocess.check_output(['git','ls-tree','-rz',M['pin']],cwd=E.parents[2]).split(b'\0'):
 if not x:continue
 meta,name=x.split(b'\t');mode,typ,oid=meta.split()
 if typ==b'blob':records.append((name.decode(),oid.decode()))
identity={}
for dirname in ['tinygo-source','supplement-source','resume-source']:
 changes=[]
 for p,oid in records:
  f=W/dirname/p; data=str(f.readlink()).encode() if f.is_symlink() else f.read_bytes()
  if hashlib.sha1(b'blob '+str(len(data)).encode()+b'\0'+data).hexdigest()!=oid:changes.append(p)
 identity[dirname]=dict(tracked_blobs=len(records),changed=changes);assert not changes
(E/'final-export-identity.json').write_text(json.dumps(identity,indent=2))
q={ 'package':'/tmp/qemu-user.deb','package_sha256':sha('/tmp/qemu-user.deb'),'emulators':{}}
for arch in ['aarch64','riscv64']:
 p=W/'qemu/usr/bin'/('qemu-'+arch);q['emulators'][arch]=dict(path=str(p),sha256=sha(p),version=subprocess.check_output([str(p),'--version'],text=True))
(E/'qemu-provenance.json').write_text(json.dumps(q,indent=2))
summary=dict(repair=M['pin'],required_tinygo_gates=46,required_tinygo_pass=46,final_regression_tinygo_modes=3,native_execution_gates=9,additional_converted_global_execution_gates=6,native_cross_build_gates=6,total_successful_gate_receipts=70,preserved_failed_test_authoring_attempts=1,production_failures=0,original_expectations_changed=False,final_approval=False,unrun=['actual Darwin','actual Windows','physical ARM64/RV64 hardware','TinyGo non-AMD64 platforms','historical broad f2bb suites'])
(E/'final-summary.json').write_text(json.dumps(summary,indent=2));print(json.dumps(summary,indent=2))
