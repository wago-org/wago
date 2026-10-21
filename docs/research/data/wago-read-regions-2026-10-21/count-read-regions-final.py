from pathlib import Path
import importlib.util,json,re,subprocess
root=Path('/home/dev/Projects/dewdrop');out=root/'.tmp/wago-next-gc-perf/read-regions/dynamic-final';out.mkdir(exist_ok=True)
spec=importlib.util.spec_from_file_location('measure',root/'docs/research/data/wago-local-perf-2026-10-20/measure.py');m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
m.DATA=out;m.ENV=dict(m.ENV,GOMAXPROCS='2',WAGO_REGION_COUNTS_OUT=str(out/'counts.json'));original=m.measure
m.measure=lambda label,cmd,**kw:original(label,['taskset','-c','12,14',*cmd],**kw)
m.full('read-regions-final-counts','read-regions-final-counts-candidate')
artifacts=list((m.WORK/'read-regions-final-counts-home').rglob('*.wago'));assert len(artifacts)==1
probe=subprocess.run([str(root/'.tmp/wago-next-gc-perf/local-admission'),str(artifacts[0])],check=True,capture_output=True,text=True);admission=json.loads(probe.stdout);assert admission['Required'] and admission['Exact'];(out/'roots.json').write_text(json.dumps(admission,indent=2))
p=json.loads((out/'counts.json').read_text());totals=[sum(row[i] for row in p['targets'].values()) for i in range(7)]
names={}
for line in (root/'.tmp/wago-next-gc-perf/read-regions/compiler.wat').read_text().splitlines():
 a=re.match(r'  \(func (\$\S+) \(;([0-9]+);\)',line)
 if a:names[a[2]]=a[1]
rows=sorted([dict(target=int(idx),name=names.get(idx,''),counts=dict(zip(p['kinds'],values))) for idx,values in p['targets'].items()],key=lambda x:x['counts']['entries'],reverse=True)
summary=dict(totals=dict(zip(p['kinds'],totals)),targets=rows,method='Separate instrumented diagnostic. Static global counter addresses are valid in this process only; fresh module/result caches required. No install or reuse in another process. Counters preserve RAX/RDX/RFLAGS and RSP. No timing claims.')
(out/'summary.json').write_text(json.dumps(summary,indent=2));print(json.dumps(dict(totals=summary['totals'],top=rows[:15])),flush=True)
