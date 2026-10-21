from pathlib import Path
import csv, hashlib, json, os, re, statistics, subprocess
root=Path('/home/dev/Projects/wago-pr881-evidence')
env={k:v for k,v in os.environ.items() if not k.startswith('WAGO_')}
env.update(GOMAXPROCS='1',WAGO_BOUNDS='explicit')
records=[]
for pair in range(1,9):
    order=['main','pr'] if pair%2 else ['pr','main']
    for ordinal,revision in enumerate(order,1):
        cmd=['taskset','-c','4',str(root/f'{revision}-amd64.test'),'-test.run=^$','-test.bench=^BenchmarkIntegerLiveAcrossJoin$','-test.benchtime=100ms','-test.count=1','-test.benchmem','-test.cpu=1']
        r=subprocess.run(cmd,env=env,capture_output=True,text=True)
        stem=f'pair-{pair:02}-{ordinal}-{revision}'
        (root/f'{stem}.stdout').write_text(r.stdout)
        (root/f'{stem}.stderr').write_text(r.stderr)
        (root/f'{stem}.exit').write_text(f'{r.returncode}\n')
        if r.returncode: raise SystemExit(r.returncode)
        rows=0
        for line in r.stdout.splitlines():
            m=re.match(r'^BenchmarkIntegerLiveAcrossJoin/(\S+)\s+\d+\s+([\d.]+) ns/op\s+([\d.]+) native-B\s+(\d+) B/op\s+(\d+) allocs/op',line)
            if not m: continue
            rows+=1
            records.append(dict(pair=pair,order=ordinal,revision=revision,benchmark=m[1],ns_per_op=float(m[2]),native_bytes=int(float(m[3])),bytes_per_op=int(m[4]),allocs_per_op=int(m[5])))
        if rows!=12: raise RuntimeError(f'{stem}: {rows} rows')
    print(f'pair {pair}/8 complete',flush=True)
with (root/'integer-join-pressure-20261019.csv').open('w') as f:
    w=csv.DictWriter(f,fieldnames=records[0].keys());w.writeheader();w.writerows(records)
summary=[]
for name in dict.fromkeys(x['benchmark'] for x in records):
    rows=[x for x in records if x['benchmark']==name]
    main=[x['ns_per_op'] for x in rows if x['revision']=='main'];pr=[x['ns_per_op'] for x in rows if x['revision']=='pr']
    delta=[(p/m-1)*100 for p,m in zip(pr,main)]
    resource={k:sorted(set(x[k] for x in rows)) for k in ['native_bytes','bytes_per_op','allocs_per_op']}
    summary.append(dict(benchmark=name,main_median=statistics.median(main),pr_median=statistics.median(pr),delta=statistics.median(delta),delta_min=min(delta),delta_max=max(delta),main_cv=100*statistics.stdev(main)/statistics.mean(main),pr_cv=100*statistics.stdev(pr)/statistics.mean(pr),**resource))
(root/'summary.json').write_text(json.dumps(summary,indent=2)+'\n')
for r in summary:
    print(f"| `{r['benchmark']}` | {r['main_median']:.2f} | {r['pr_median']:.2f} | {r['delta']:+.2f}% [{r['delta_min']:+.2f}%, {r['delta_max']:+.2f}%] | {r['main_cv']:.2f}% / {r['pr_cv']:.2f}% |")
