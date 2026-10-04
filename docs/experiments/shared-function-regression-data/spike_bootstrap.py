from pathlib import Path
import random,statistics,json
p=Path(__file__).parent;rng=random.Random(802);out={}
for key in ['SharingExec/join','Exec/tiny.add']:
 def samples(rev):return [float(l.split()[2])for l in (p/'results'/(rev+'-combined-spikes.txt')).read_text().splitlines()if l.startswith('Benchmark'+key+' ')]
 a,b=samples('M'),samples('F');ratios=sorted(100*(statistics.median(rng.choices(b,k=len(b)))/statistics.median(rng.choices(a,k=len(a)))-1)for _ in range(50000))
 out[key]=dict(M=a,F=b,relative_median_percent=100*(statistics.median(b)/statistics.median(a)-1),bootstrap_percentile_95_percent=[ratios[1250],ratios[48749]],seed=802,resamples=50000,n_per_revision=len(a))
(p/'spike-bootstrap.json').write_text(json.dumps(out,indent=2))
