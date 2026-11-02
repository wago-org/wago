import hashlib,json,os,pathlib,subprocess,tempfile
root=pathlib.Path.cwd(); out=root/'docs/rfwgc-evidence/darwin-reservation-checkptr-repair'
p=json.loads((root/'docs/rfwgc-evidence/qemu-arm64-final-diagnosis/provenance.json').read_text())
work=pathlib.Path(tempfile.mkdtemp(prefix='_wago_reservation_checkptr_',dir='/home/dev/.cache'))
rel='src/wago/gc_native_reservation_arm64_test.go'
copy=work/'reservation_test.go';copy.write_bytes((root/rel).read_bytes())
overlay=work/'overlay.json';overlay.write_text(json.dumps({'Replace':{str(pathlib.Path(p['export'])/rel):str(copy)}}))
env=p['environment']; results=[]
for mode in ['ordinary','wagodebug','wago_regalloccheck']:
 binary=work/(mode+'.test')
 cmd=[p['go'],'test','-c','-p=2','-gcflags=all=-d=checkptr=2','-overlay',str(overlay),'-o',str(binary)]
 if mode!='ordinary':cmd+=['-tags='+mode]
 cmd+=['./src/wago']
 for kind,args in [('build',cmd),('execute',[p['qemu'],'-cpu','max','-t','32 36 28,64 64 1',str(binary),'-test.v','-test.count=1','-test.timeout=3m','-test.run=^TestGCARM64(LiveStructDoesNotPrepareUnusedHandles|WideArrayDoesNotPrepareUnconsumableState)$'])]:
  log=out/(mode+'-'+kind+'.log')
  with log.open('wb') as f:r=subprocess.run(args,cwd=p['export'],env=env,stdout=f,stderr=subprocess.STDOUT,timeout=300)
  results.append({'mode':mode,'kind':kind,'argv':args,'exit':r.returncode,'log_sha256':hashlib.sha256(log.read_bytes()).hexdigest()})
  (out/'results.json').write_text(json.dumps(results,indent=2)+'\n')
  print(mode,kind,r.returncode,flush=True)
  if r.returncode:raise SystemExit(r.returncode)
(out/'provenance.json').write_text(json.dumps({'base':p['pin'],'base_export':p['export'],'overlay_path':str(copy),'overlay_sha256':hashlib.sha256(copy.read_bytes()).hexdigest(),'environment':env,'go':p['go'],'qemu':p['qemu'],'scope':'ARM64 QEMU with checkptr=2; not native Darwin or race qualification'},indent=2)+'\n')
