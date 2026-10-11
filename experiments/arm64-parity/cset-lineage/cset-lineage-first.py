from pathlib import Path
import os,subprocess,json,statistics
out=Path('/tmp/cset-lineage-first');out.mkdir(exist_ok=True)
e=os.environ.copy();e.update(GOWORK='off',WAGO_SHARED_SCALAR='0',WAGO_ARM64_EXPERIMENT_CSET_LINEAGE='1')
subprocess.run(['go','build','-tags','wago_guardpage','-o','/tmp/paired-cset-lineage','./experiments/arm64-parity/paired'],cwd='/tmp/wago-cset-lineage-76e52be8',env=e,check=True)
rows=[]
for phase,names in [('exec','ml-kmeans'),('compile','ml-kmeans,audio-adpcm,files-glob-match,geo-point-in-polygon,video-dct')]:
 cmd=['/tmp/paired-cset-lineage','-option','cset-lineage','-workloads',names,'-phase',phase,'-rounds','6','-budget','100ms']
 if phase=='exec':cmd+=['-code-dir',str(out/'images')]
 p=out/(phase+'.raw')
 with p.open('w') as f:subprocess.run(cmd,env=e,stdout=f,stderr=subprocess.STDOUT,check=True)
 rows += [json.loads(l) for l in p.read_text().splitlines() if l.startswith('{')]
 (out/'all.jsonl').write_text(''.join(json.dumps(x)+'\n' for x in rows))
lines=['|workload|phase|paired change|OFF us|ON us|','|---|---|---:|---:|---:|']
for name,phase in sorted({(r['workload'],r['phase']) for r in rows}):
 d={(r['round'],r['on']):r['us'] for r in rows if r['workload']==name and r['phase']==phase}
 ratio=statistics.median(100*(d[(i,True)]/d[(i,False)]-1) for i in range(6))
 lines.append(f'|{name}|{phase}|{ratio:+.2f}%|{statistics.median(d[(i,False)] for i in range(6)):.3f}|{statistics.median(d[(i,True)] for i in range(6)):.3f}|')
(out/'summary.md').write_text('\n'.join(lines)+'\n');print('\n'.join(lines),flush=True)
p=out/'images/ml-kmeans-0.bin';q=out/'images/ml-kmeans-1.bin';a=p.read_bytes();b=q.read_bytes();assert len(a)==len(b)
changes=[(i,int.from_bytes(a[i:i+4],'little'),int.from_bytes(b[i:i+4],'little')) for i in range(0,len(a),4) if a[i:i+4]!=b[i:i+4]]
(out/'changes.txt').write_text(''.join(f'{i:x}: {x:08x} → {y:08x}\n' for i,x,y in changes));print('changed words',len(changes),flush=True)
