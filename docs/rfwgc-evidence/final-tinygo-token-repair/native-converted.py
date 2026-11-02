import pathlib,json,subprocess,hashlib,re
E=pathlib.Path(__file__).resolve().parent
D=hashlib.sha256(pathlib.Path(__file__).read_bytes()).hexdigest(); results=[]
assert not (E/'SAFETY-STOP.json').exists()
for arch,required in [('arm64',['TestR9H2HostContainerPublicGlobal','TestR9H2HostImportGlobal','TestR9H2HostImportGlobalOrdinaryAndGuards']),('riscv64',['TestRV64CompactExternHostGlobalPrivate'])]:
 for mode in ['ordinary','wagodebug','wago_regalloccheck']:
  receipt=json.loads((E/('resume-native-'+arch+'-'+mode+'.command.json')).read_text())
  label='converted-'+arch+'-'+mode
  binary=pathlib.Path(receipt['argv'][3]); bm=json.loads((E/('native-'+arch+'-'+mode+'.binary.json')).read_text()); assert hashlib.sha256(binary.read_bytes()).hexdigest()==bm['sha256']; assert bm['elf_machine']=={'arm64':183,'riscv64':243}[arch]
  receipt['argv']=[x for x in receipt['argv'] if not x.startswith('-test.run=')]+['-test.run=^('+'|'.join(required)+')$']
  receipt['driver_sha256']=D
  (E/(label+'.command.json')).write_text(json.dumps(receipt,indent=2))
  with (E/(label+'.log')).open('w') as f:
   try:rc=subprocess.run(receipt['argv'],cwd=receipt['cwd'],env=receipt['environment'],stdout=f,stderr=subprocess.STDOUT,timeout=900).returncode
   except subprocess.TimeoutExpired:rc=124
  raw=(E/(label+'.log')).read_bytes(); text=raw.decode(errors='replace')
  passed=re.findall(r'^\s*--- PASS: (\S+)',text,re.M); skipped=re.findall(r'^\s*--- SKIP: (\S+)',text,re.M); failed=re.findall(r'^\s*--- FAIL: (\S+)',text,re.M)
  row=dict(label=label,exit=rc,passed=passed,skipped=skipped,failed=failed,missing=[x for x in required if x not in passed],raw_sha256=hashlib.sha256(raw).hexdigest())
  results.append(row);(E/'native-converted-results.json').write_text(json.dumps(results,indent=2));print(label,rc,len(passed),flush=True)
  if rc or skipped or failed or row['missing']:
   (E/'SAFETY-STOP.json').write_text(json.dumps(row,indent=2));raise SystemExit(1)
assert D==hashlib.sha256(pathlib.Path(__file__).read_bytes()).hexdigest()
