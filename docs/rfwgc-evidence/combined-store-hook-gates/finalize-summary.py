#!/usr/bin/env python3
import pathlib,json,gzip,runpy,re,hashlib
E=pathlib.Path(__file__).resolve().parent
runpy.run_path(str(E/'summarize.py'),run_name='__main__')
rows=json.loads((E/'summary.json').read_text());pin=json.loads((E/'source.json').read_text())['pin']
for f in sorted(E.rglob('command.json')):
 if (f.parent/'result.json').exists():continue
 d=json.loads(f.read_text());name=str(f.parent.relative_to(E));raw=f.parent/'raw.log';raw=raw if raw.exists() else f.parent/'raw.log.gz'
 rows.append({'suite':name,'source':d['source'],'exit':None,'verdict':'INTERRUPTED','reason':'Harness isolation interruption' if name=='build-wagodebug' else 'Stopped at combined-source unsafe qualification gate; no completion exit observed.','log':str(raw.relative_to(E)) if raw.exists() else None})
for name in ['qemu-collector','qemu-guest']:
 out=E/name;done={r['label'] for r in json.loads((out/'results.json').read_text())}
 for f in sorted(out.glob('*.log')):
  if f.stem not in done:rows.append({'suite':name+'/'+f.stem,'source':pin,'exit':None,'verdict':'INTERRUPTED','reason':'Stopped at combined-source unsafe qualification gate.','log':str(f.relative_to(E))})
out=E/'tinygo-34';done={}
for l in (out/'qualification-results.txt').read_text().splitlines():
 n,code=l.split(' exit=');done[n]=int(code)
names=[x+'-'+m for m in ['ordinary','wagodebug','wago_regalloccheck'] for x in ['host','standalone','guest-threads']]+['ci-checkers','ci-runtime','ci-cli','ci-cli-smoke','ci-release']+[x+'-'+str(i) for i in range(1,11) for x in ['ci-release-version','ci-release-fib']]
for name in names:
 code=done.get(name);started=(out/(name+'.command')).exists()
 rows.append({'suite':'tinygo-34/'+name,'source':pin,'exit':code,'verdict':('PASS' if code==0 else 'FAIL') if name in done else ('INTERRUPTED' if started else 'NOT_RUN'),'reason':None if name in done else 'Stopped at combined-source unsafe qualification gate.','skip_census':'Original nonverbose driver does not enumerate individual test skips; no zero-skip claim.','log':str((out/(name+'.log')).relative_to(E)) if started else None})
expected=[]
for m in ['ordinary','wagodebug','wago_regalloccheck']:
 expected += ['full-root-'+m,'cli-'+m,'installer-'+m,'nested-'+m,'collector-'+m,'race-'+m,'guard-'+m,'corpus-'+m,'baseline-corpus-'+m,'graphs-and-explicit-catalog-port-'+m,'graphs-and-explicit-catalog-baseline-'+m,'tinygo-evacuation-'+m,'tinygo-joint-'+m,'spec-v3-signals-'+m,'guard-corpus-'+m,'runtime-cli-build-'+m,'profile-metadata-'+m,'profile-runtime-'+m,'shared-scalar-'+m,'native-join-'+m,'seeded-concurrency-'+m]
 for arch in ['arm64','riscv64']:expected+=['qemu-guest/'+arch+'-'+m+'-guest']
for tags in ['wago_codegenstats','wagodebug,wago_codegenstats','wago_regalloccheck,wago_codegenstats']:expected+=['default-diagnostic-complete-'+tags]
for n in expected:
 if not any(r['suite']==n for r in rows):rows.append({'suite':n,'source':pin,'exit':None,'verdict':'NOT_RUN','reason':'Not reached before unsafe source blockers stopped qualification.'})
rows.append({'suite':'native-Darwin','source':pin,'exit':None,'verdict':'NOT_RUN','reason':'No native Darwin execution endpoint available; Linux QEMU does not qualify Darwin.'})
(E/'qualification.json').write_text(json.dumps({'source':pin,'aggregate':'BLOCKED','rows':rows,'scope_exclusions':['Host-created converted containers pending scope/support map','TinyGo and other-host opaque receipt hooks remain excluded','No future source head qualified','No performance/review closure']},indent=2)+'\n')
print('Final rows',len(rows))
