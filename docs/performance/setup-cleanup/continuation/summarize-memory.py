from pathlib import Path
import json,re,statistics as st
p=Path(__file__).resolve().parent;out={}
for engine in ['wago-guarded','wazero-default','wazero-max-capacity']:
 for pages in [6,16]:
  for data in [0,4096,65536,pages*65536]:
   rows=[]
   for l in (p/f'memory-policy-released-{pages}-{data}-{engine}.txt').read_text().splitlines():
    if '{"' in l:
     r=json.loads(l[l.index('{"'):]);r['rss_kib']=int(re.search(r'^Rss:\s+(\d+)',r['smaps'],re.M)[1]);rows.append(r)
   key=f'{pages}/{data}/{engine}';out[key]={}
   for phase in ['compiled-normal','first-closed-normal','warm-closed-normal','released-normal','released-gc']:
    group=[r for r in rows if r['phase']==phase];out[key][phase]={k:st.median(r[k] for r in group) for k in ['rss_kib','heap_alloc','minor_faults','major_faults']}
(p/'memory-policy-released-summary.json').write_text(json.dumps(out,indent=2)+'\n')
for key,val in out.items():
 if key.startswith('16/1048576/') or key.startswith('6/0/'): print(key,val)
