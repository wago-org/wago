#!/usr/bin/env python3
import json,re,statistics
from pathlib import Path
root=Path(__file__).resolve().parent
cells={}
for p in sorted(root.glob('[0-9][0-9]-*.txt')):
    _,suite,revision,*_=p.stem.split('-')
    for line in p.read_text().splitlines():
        if not line.startswith('BenchmarkGCLifetime'): continue
        tokens=line.split()
        name=re.sub(r'-\d+$','',tokens[0])
        metrics={tokens[i+1]:float(tokens[i]) for i in range(2,len(tokens)-1,2)}
        cells.setdefault((suite,name,revision),[]).append(metrics)
assert len(cells)==24 and all(len(v)==6 for v in cells.values()), 'Need six samples for every required cell'
def summary(values):
    return f'{statistics.median(values):.3f} [{min(values):.3f}, {max(values):.3f}]; {100*statistics.stdev(values)/statistics.mean(values):.2f}%' if len(values)>1 else str(values)
lines=['# Stage2 measured comparison','', 'Each timing cell: median [min, max]; sample CV. Time unit ns/op; lower is faster. Percentages compare medians.','', '| Suite / fixture / option | n baseline/stage2 | Baseline ns/op [min,max]; CV | Stage2 ns/op [min,max]; CV | Stage2 vs baseline | Host B/op baseline → stage2 | Host allocs/op baseline → stage2 |','|---|---:|---|---|---:|---:|---:|']
for suite,name,rev in sorted(cells):
    if rev!='baseline':continue
    b=cells[suite,name,'baseline']; s=cells[suite,name,'stage2']
    med=lambda rows,k:statistics.median(x[k] for x in rows)
    delta=(med(s,'ns/op')/med(b,'ns/op')-1)*100
    lines.append(f'| {suite} / {name.split("/",1)[1]} | {len(b)}/{len(s)} | {summary([x["ns/op"] for x in b])} | {summary([x["ns/op"] for x in s])} | {delta:+.2f}% | {med(b,"B/op"):g} → {med(s,"B/op"):g} | {med(b,"allocs/op"):g} → {med(s,"allocs/op"):g} |')
lines+=['','Within-revision On vs Off median runtime/compile costs:','','| Suite / fixture | Baseline On vs Off | Stage2 On vs Off |','|---|---:|---:|']
for suite,name,rev in sorted(cells):
    if rev!='baseline' or not (name.endswith('/off') or name.endswith('/on=false')):continue
    on=name[:-3]+'on' if name.endswith('/off') else name[:-5]+'true'
    def diff(r):return (statistics.median(x['ns/op'] for x in cells[suite,on,r])/statistics.median(x['ns/op'] for x in cells[suite,name,r])-1)*100
    lines.append(f'| {suite} / {name.split("/",1)[1].rsplit("/",1)[0]} | {diff("baseline"):+.2f}% | {diff("stage2"):+.2f}% |')
lines+=['','Guest accounting (separate fresh instance, exactly 1024 invocations; NOT timed). All repeated observations must agree. Totals below are exact counters reconstructed from per-op values when representable; raw Go output rounds the fractions.','','| Revision / runtime cell | Charges | Handleless | Materialized / published | Layout B | Observed nursery advance B | Retained nursery B | Rounded heap request B (calculated) |','|---|---:|---:|---:|---:|---:|---:|---:|']
for (suite,name,rev),rows in sorted(cells.items()):
    if suite!='runtime':continue
    keys=['guest-charges/op','handleless/op','materialized/op','published-handles/op','layout-B/op','observed-nursery-advance-B/op','retained-nursery-B','throughput-rounded-B/op']
    assert all(all(r[k]==rows[0][k] for k in keys) for r in rows)
    r=rows[0]; total=lambda k:str(round(r[k]*1024))
    lines.append(f'| {rev} / {name.split("/",1)[1]} | {total(keys[0])} | {total(keys[1])} | {total(keys[2])} / {total(keys[3])} | {total(keys[4])} | {total(keys[5])} | {r[keys[6]]:g} | '+(total(keys[7]) if '/throughput/' in name else 'N/A')+' |')
(root/'tables.md').write_text('\n'.join(lines)+'\n')
(root/'summary.json').write_text(json.dumps({'|'.join(k):v for k,v in cells.items()},indent=2)+'\n')
print('\n'.join(lines))
# Verify every accepted output is backed by quiet telemetry; rejected outputs
# never enter the statistics above.
rows=[json.loads(l) for l in (root/'during-timing.jsonl').read_text().splitlines()]
accepted=[x for x in rows if x['accepted']]
assert len(accepted)==72 and len({x['cell'] for x in accepted})==72
for x in accepted:
    assert (root/(x['cell']+'.txt')).exists()
    assert x['observations']
    assert all(not o['noisy'] and o['external_host_pct']<=5 and
               max(o['watched_cpu_busy_pct'].values(),default=0)<=5 for o in x['observations'])
