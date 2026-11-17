#!/usr/bin/env python3
"""Fresh-process paired probe trials; input manifest entries need id/path/sha256."""
import argparse, hashlib, json, os, pathlib, subprocess
p=argparse.ArgumentParser()
p.add_argument('--manifest',required=True)
p.add_argument('--baseline',required=True)
p.add_argument('--candidate')
p.add_argument('--trials',type=int,default=5)
p.add_argument('--out',required=True)
a=p.parse_args()
corpus=json.loads(pathlib.Path(a.manifest).read_text())
for c in corpus:
    if hashlib.sha256(pathlib.Path(c['path']).read_bytes()).hexdigest()!=c['sha256']:
        raise SystemExit('artifact mismatch: '+c['id'])
binaries={'baseline':a.baseline}
if a.candidate:binaries['candidate']=a.candidate
provenance={k:hashlib.sha256(pathlib.Path(v).read_bytes()).hexdigest() for k,v in binaries.items()}
with open(a.out,'w') as output:
    for trial in range(a.trials):
        order=list(binaries)
        if trial%2:order.reverse()
        for c in corpus:
            for variant in order:
                proc=subprocess.run([binaries[variant],c['path']],capture_output=True,text=True,env=dict(os.environ,GOMAXPROCS='1'))
                row={'id':c['id'],'trial':trial,'variant':variant,'binary_sha256':provenance[variant],'status':proc.returncode}
                if proc.returncode:row['error']=proc.stderr
                else:
                    row.update(json.loads(proc.stdout))
                    if row['sha256']!=c['sha256']:raise SystemExit('probe artifact mismatch')
                output.write(json.dumps(row)+'\n');output.flush()
