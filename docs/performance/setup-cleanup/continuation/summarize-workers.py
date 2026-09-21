from pathlib import Path
import json,re,statistics as st
p=Path(__file__).resolve().parent
files={k:[] for k in ['1','2','4','auto','raw-zero']}; counts={}
for api in ['raw','policy','public']:
 for l in (p/f'workers-owned-{api}.txt').read_text().splitlines():
  m=re.match(r'^BenchmarkWorkerLifecycleDiagnostic/(.*)/callers=(\d+)/(validate|backend|full)/(raw|policy|public)/requested=(\d+)-16\s+(.*)',l)
  if not m:continue
  module,callers,stage,path,request,rest=m.groups();tokens=rest.split();metrics=dict(zip(tokens[2::2],tokens[1::2]))
  mode=('raw-zero' if path=='raw' else 'auto') if request=='0' else request
  name=f'Worker/{module}/callers={callers}/{stage}'
  files[mode].append(f'Benchmark{name} {tokens[0]} {metrics["ns/op"]} ns/op {metrics["B/op"]} B/op {metrics["allocs/op"]} allocs/op')
  counts[f'{name}/{path}/{request}']={k:float(v) for k,v in metrics.items() if k not in ['ns/op','B/op','allocs/op']}
for mode,lines in files.items(): (p/f'workers-mode-{mode}.txt').write_text('\n'.join(lines)+'\n')
(p/'workers-effective-counts.json').write_text(json.dumps(counts,indent=2)+'\n')
resources=[json.loads(l) for l in (p/'workers-owned-resources.jsonl').read_text().splitlines()]
summary={}
for module in ['tiny','many_funcs','json-as','lua']:
 for callers in [1,4,16]:
  for workers in [1,2,4,0]:
   key=f'{module}-{callers}-{workers}'
   group=[r for r in resources if r['kind']=='worker-memory' and r['label']==key]
   record={k:{'median':st.median(r[k] for r in group),'max':max(r[k] for r in group)} for k in ['peak_rss_kib','peak_virtual_kib','peak_mapping_count','minor_faults','major_faults']}
   vals={}
   for l in (p/f'worker-memory-{key}.txt').read_text().splitlines():
    for k in ['individual_p50_ns','individual_max_ns','aggregate_ns_per_call']:
     m=re.search(r'\b'+k+r'=(\d+)',l)
     if m: vals.setdefault(k,[]).append(int(m[1]))
   record.update({k:st.median(v) for k,v in vals.items()});summary[key]=record
(p/'worker-memory-summary.json').write_text(json.dumps(summary,indent=2)+'\n')
for k in ['lua-1-1','lua-1-4','lua-16-1','lua-16-4','json-as-4-1','json-as-4-4']:print(k,summary[k])
