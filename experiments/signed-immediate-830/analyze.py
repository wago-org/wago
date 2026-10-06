#!/usr/bin/env python3
import csv
import math
import re
import statistics
from pathlib import Path
root=Path(__file__).resolve().parent

def read(path):
    result={}
    for line in path.read_text().splitlines():
        if not line.startswith('Benchmark'): continue
        f=line.split()
        if len(f)<4 or f[3]!='ns/op': continue
        name=re.sub(r'-\d+$','',f[0])
        result.setdefault(name,[]).append({f[i+1]:float(f[i]) for i in range(2,len(f)-1,2)})
    return result

rows=[]
for profile in ['modern','sse2','corpus']:
    baseline,candidate=[read(root/f'{form}-{profile}.txt') for form in ['baseline','candidate']]
    assert baseline.keys()==candidate.keys()
    for name,samples in baseline.items():
        other=candidate[name]
        assert len(samples)==len(other)==7,(name,len(samples),len(other))
        row={'profile':profile,'benchmark':name,'samples_per_form':7}
        for metric in ['ns/op','ns/store','B/op','allocs/op','code-B']:
            if metric not in samples[0]: continue
            a,b=[statistics.median(x[metric] for x in s) for s in [samples,other]]
            row['baseline_'+metric],row['candidate_'+metric]=a,b
        row['candidate_change_percent']=(row['candidate_ns/op']/row['baseline_ns/op']-1)*100
        rows.append(row)
    for kind,prefix in [('execution','BenchmarkSignedImmediateStore'),('compile','BenchmarkCompileSignedImmediateStore'),('corpus execution','BenchmarkExec/'),('command','BenchmarkCommandExec/'),('full compile','BenchmarkCompileFull/')]:
        group=[r for r in rows if r['profile']==profile and r['benchmark'].startswith(prefix)]
        if group:
            ratio=math.exp(statistics.mean(math.log(r['candidate_ns/op']/r['baseline_ns/op']) for r in group))
            print(profile,kind,len(group),'rows geomean',f'{(ratio-1)*100:+.2f}%')
            for r in group: print(' ',r['benchmark'],f'{r["baseline_ns/op"]:.1f} -> {r["candidate_ns/op"]:.1f} ns/op',f'{r["candidate_change_percent"]:+.2f}%')
columns=list(dict.fromkeys(k for r in rows for k in r))
with (root/'summary.csv').open('w',newline='') as f:
    w=csv.DictWriter(f,fieldnames=columns);w.writeheader();w.writerows(rows)
