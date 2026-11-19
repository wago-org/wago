from pathlib import Path
import json,gzip,random,statistics,math,hashlib,collections
import argparse,sys
from prepare import KIT,sha,require
p=argparse.ArgumentParser();p.add_argument('--run-directory',type=Path,required=True);a=p.parse_args()
require(not sys.flags.optimize,'do not use python -O')
D=a.run_directory
manifest=json.loads((D/'PRELAUNCH.json').read_text())
journal=[json.loads(x) for x in (D/'journal.jsonl').read_text().splitlines()]
require(journal[0]['stage']=='start' and journal[0]['frozen_sha256']==sha(D/'PRELAUNCH.json') and journal[-1]['stage']=='complete','run not attested complete')
require(journal[-1]['raw_sha256']==sha(D/'records.jsonl') and journal[-1]['gzip_sha256']==sha(D/'records.jsonl.gz'),'record integrity mismatch')
require(manifest['kit']['analyze.py']==sha(Path(__file__)) and manifest['kit']['inputs.json']==sha(KIT/'inputs.json'),'analysis or contracts differ from frozen kit')
rows=[json.loads(x) for x in gzip.open(D/'records.jsonl.gz','rt')]
contracts={x['id']:x['sha256'] for x in json.loads((KIT/'inputs.json').read_text())}
ids=list(contracts)
strata=['rewrite','no-rewrite'];rng=random.Random(992271)
job_rng=random.Random(902173)
expected=[]
for epoch in range(3):
 blocks_in_epoch=[(w,s,b,'ABBA' if b<2 else 'BAAB') for w in ids for s in strata for b in range(4)]
 job_rng.shuffle(blocks_in_epoch)
 for index,(w,s,b,seq) in enumerate(blocks_in_epoch):
  expected.extend((w,label,epoch,s,b,seq,pos,index) for pos,label in enumerate(seq,1))
require(len(rows)==384 and all(x['verified'] for x in rows),'wrong count or missing oracle')
for x,want in zip(rows,expected):
 d=x['diagnostic']
 have=(x['id'],x['variant'],d['epoch'],d['stratum'],d['within_cell_block'],d['sequence'],d['position'],d['block_index'])
 require(have==want and x['binary_sha256']==manifest['binary_sha256'] and x['elapsed_ns']>0 and x['artifact_sha256']==contracts[x['id']] and x['cpu']==manifest['cpu'] and x['slot']['sha256']==manifest['binary_sha256'],'row identity or elapsed mismatch')
require(len({(x['slot']['device'],x['slot']['inode']) for x in rows})==1,'slot inode changed')
def quant(v,q):s=sorted(v);return s[min(len(s)-1,int(q*(len(s)-1)))]
def spread(v):return {'n':len(v),'median':statistics.median(v),'p10':quant(v,.1),'p90':quant(v,.9),'p95':quant(v,.95),'max':max(v)}
def ratio(rs):return statistics.median(x['elapsed_ns'] for x in rs if x['variant']=='A')/statistics.median(x['elapsed_ns'] for x in rs if x['variant']=='B')
blocks={};point={};draws={};agg={s:[] for s in strata};cross=[]
for s in strata:
 for w in ids:
  rs=[x for x in rows if x['id']==w and x['diagnostic']['stratum']==s];g=collections.defaultdict(list)
  for x in rs:g[x['diagnostic']['epoch'],x['diagnostic']['within_cell_block']].append(x)
  assert len(g)==12 and all(len(v)==4 and sorted(x['variant'] for x in v)==['A','A','B','B'] for v in g.values());blocks[s,w]=list(g.values());point[s,w]=ratio(rs);draws[s,w]=[]
for _ in range(5000):
 med={}
 for s in strata:
  for w in ids:
   b=blocks[s,w];rs=[x for i in [rng.randrange(12) for _ in range(12)] for x in b[i]];med[s,w]={a:statistics.median(x['elapsed_ns'] for x in rs if x['variant']==a) for a in ['A','B']};draws[s,w].append(med[s,w]['A']/med[s,w]['B'])
  agg[s].append(statistics.mean(med[s,w]['A'] for w in ids)/statistics.mean(med[s,w]['B'] for w in ids))
 cross.append(statistics.mean((med['rewrite',w]['A']+med['rewrite',w]['B'])/2 for w in ids)/statistics.mean((med['no-rewrite',w]['A']+med['no-rewrite',w]['B'])/2 for w in ids))
def interval(v):s=sorted(v);return[s[125],s[4875]]
def gate(v,c):return abs(v-1)<=.02 and c[0]>=.97 and c[1]<=1.03
def record(v,d):c=interval(d);return {'A_B':v,'paired_block95':c,'pass':gate(v,c)}
results={}
for s in strata:
 cases=[dict(id=w,**record(point[s,w],draws[s,w])) for w in ids];ms={a:statistics.mean(statistics.median(x['elapsed_ns'] for x in rows if x['id']==w and x['diagnostic']['stratum']==s and x['variant']==a) for w in ids) for a in ['A','B']};a=record(ms['A']/ms['B'],agg[s]);results[s]={'cases':cases,'aggregate':a,'all_cases_and_aggregate_pass':a['pass'] and all(x['pass'] for x in cases)}
# Descriptive only: retain each workload/stratum/epoch/position/transition group.
mednorm={(s,w):statistics.median(x['elapsed_ns'] for x in rows if x['id']==w and x['diagnostic']['stratum']==s) for s in strata for w in ids}
groups=collections.defaultdict(list)
for i,x in enumerate(rows):
 m=x['diagnostic'];transition='first' if i==0 else ('changed' if rows[i-1]['diagnostic']['stratum']!=m['stratum'] else 'same');val=x['elapsed_ns']/mednorm[m['stratum'],x['id']]
 for k in [('epoch',m['stratum'],m['epoch']),('position',m['stratum'],m['position']),('transition',m['stratum'],transition),('sequence',m['stratum'],m['sequence']),('workload_epoch',x['id'],m['stratum'],m['epoch'])]:groups[str(k)].append(val)
def delta(x,k):p={z['stage']:z for z in x['phase_process_stats']};return p['compiled'][k]-p['before_compile'][k]
def corr(a,b):
 if len(a)<2 or len(set(a))<2 or len(set(b))<2:return None
 ma,mb=statistics.mean(a),statistics.mean(b);return sum((x-ma)*(y-mb) for x,y in zip(a,b))/(sum((x-ma)**2 for x in a)*sum((y-mb)**2 for y in b))**.5
correlations={};availability=collections.Counter()
for s in strata:
 rs=[x for x in rows if x['diagnostic']['stratum']==s];ys=[math.log(x['elapsed_ns']/mednorm[s,x['id']]) for x in rs]
 for k in ['minor_faults','major_faults','nonvoluntary_ctxt_switches']:
  vals=[delta(x,k) for x in rs];correlations[s+'/'+k]={'distribution':spread(vals),'pearson_log_elapsed':corr(vals,ys)}
 for n,key in enumerate(['runtime_ns','runqueue_wait_ns','timeslices']):
  vals=[];yv=[]
  for x,y in zip(rs,ys):
   p={z['stage']:z for z in x['phase_process_stats']}
   if all('schedstat' in p[z] and len(p[z]['schedstat'])>=3 for z in ['before_compile','compiled']):vals.append(p['compiled']['schedstat'][n]-p['before_compile']['schedstat'][n]);yv.append(y)
  correlations[s+'/schedstat_'+key]={'distribution':spread(vals) if vals else None,'pearson_log_elapsed':corr(vals,yv)}
for x in rows:
 for p in x['phase_process_stats']:
  availability['schedstat_present' if 'schedstat' in p else 'schedstat_missing']+=1;availability['host_cpu_present' if p.get('host_cpu') else 'host_cpu_missing']+=1
means={s:statistics.mean(statistics.mean(statistics.median(x['elapsed_ns'] for x in rows if x['id']==w and x['diagnostic']['stratum']==s and x['variant']==a) for a in ['A','B']) for w in ids) for s in strata};out={'verified':384,'controls':results,'rewrite_over_no_rewrite':{'point_ratio_of_mean_label_medians':means['rewrite']/means['no-rewrite'],'paired_block95_mean_label_medians':interval(cross),'note':'Different strata resampled independently; descriptive protocol effect, not corrected qualification metric.'},'descriptive_normalized_distributions':{k:spread(v) for k,v in groups.items()},'correlations':correlations,'availability':dict(availability),'raw_elapsed_by_cell':{s+'/'+w:spread([x['elapsed_ns'] for x in rows if x['id']==w and x['diagnostic']['stratum']==s]) for s in strata for w in ids},'hashes':{n:hashlib.sha256((D/n).read_bytes()).hexdigest() for n in ['PRELAUNCH.json','journal.jsonl','records.jsonl','records.jsonl.gz']}}
with (D/'summary.json').open('x') as f: f.write(json.dumps(out,indent=2)+'\n')
print(json.dumps({'controls':results,'correlations':correlations,'availability':dict(availability),'rewrite':out['rewrite_over_no_rewrite']},indent=2))
