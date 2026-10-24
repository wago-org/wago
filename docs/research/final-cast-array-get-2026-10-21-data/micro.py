import hashlib,json,os,re,statistics,subprocess,time
from pathlib import Path
root=Path(__file__).resolve().parents[3];d=Path(__file__).resolve().parent
bins={k:Path('/tmp/wago-final-cast-array-get-'+k+'.test') for k in ['control','candidate']}
env=os.environ.copy();env['GOMAXPROCS']='1';env['CGO_ENABLED']='0'
identity={'source_commit':subprocess.check_output(['git','rev-parse','HEAD'],cwd=root,text=True).strip(),'base':'cd029409bd8985ab51d30e5f68714d13d00192fa','go':subprocess.check_output(['go','version'],text=True).strip(),'cpu':15,'GOMAXPROCS':1,'benchtime':'200ms','blocks':3,'order':'ABBA','A':'control','B':'candidate','fixture_sha256':hashlib.sha256((root/'tests/fixtures/wasm/gc-final-cast-array-get.wasm').read_bytes()).hexdigest(),'binaries':{k:{'path':str(p),'sha256':hashlib.sha256(p.read_bytes()).hexdigest()} for k,p in bins.items()},'source_patch_sha256':hashlib.sha256((d/'candidate-source.patch').read_bytes()).hexdigest(),'production_patch_sha256':hashlib.sha256((d/'production.patch').read_bytes()).hexdigest()}
(d/'micro-identity.json').write_text(json.dumps(identity,indent=2)+'\n')
rows=[]
for block in range(3):
 for pos,mode in enumerate(['control','candidate','candidate','control']):
  cmd=['taskset','-c','15',str(bins[mode]),'-test.run=^$','-test.bench=^BenchmarkGCFinalCastArrayGet$','-test.benchtime=200ms','-test.count=1']
  name=f'micro-{block}-{pos}-{mode}';start=time.monotonic()
  with (d/(name+'.log')).open('w') as out:p=subprocess.run(cmd,cwd=root/'src/wago',env=env,stdout=out,stderr=subprocess.STDOUT)
  meta={'block':block,'position':pos,'mode':mode,'command':cmd,'seconds':time.monotonic()-start,'exit_code':p.returncode}
  (d/(name+'.json')).write_text(json.dumps(meta,indent=2)+'\n');print(json.dumps(meta),flush=True)
  log=(d/(name+'.log')).read_text()
  found=re.findall(r'BenchmarkGCFinalCastArrayGet/([^\s-]+(?:-[^\s]+)?)\s+(\d+)\s+([\d.]+) ns/op\s+(\d+) B/op\s+(\d+) allocs/op',log)
  if p.returncode or len(found)!=6:raise SystemExit(f'{name}: expected6 samples, found{len(found)} exit{p.returncode}')
  for case,it,ns,bytes_,allocs in found:rows.append({**meta,'case':case,'iterations':int(it),'ns_op':float(ns),'bytes_op':int(bytes_),'allocs_op':int(allocs)})
  (d/'micro-samples.json').write_text(json.dumps(rows,indent=2)+'\n')
summary={}
for case in sorted({r['case'] for r in rows}):
 groups={mode:[r['ns_op'] for r in rows if r['case']==case and r['mode']==mode] for mode in bins}
 c,b=(statistics.median(groups[k]) for k in ['control','candidate'])
 summary[case]={'control':groups['control'],'candidate':groups['candidate'],'control_median_ns':c,'candidate_median_ns':b,'change_percent':100*(b/c-1),'unit':'one Invoke, one small entry' if case=='pin-entry' else 'one Invoke,8192 guest iterations'}
(d/'micro-summary.json').write_text(json.dumps(summary,indent=2)+'\n');print(json.dumps(summary),flush=True)
