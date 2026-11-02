#!/usr/bin/env python3
"""Freeze a PREPARATION_ONLY manifest. Does not authorize or execute acquisition."""
import pathlib,json,hashlib,random,collections,re
H=pathlib.Path(__file__).resolve().parent
MODES={'ordinary','wagodebug','wago_regalloccheck'}
def sha(p):
 d=hashlib.sha256()
 with pathlib.Path(p).open('rb') as f:
  for block in iter(lambda:f.read(1048576),b''):d.update(block)
 return d.hexdigest()
def read(p):return json.loads((H/p).read_text())
def write(p,x):(H/p).write_text(json.dumps(x,indent=2)+'\n')
p=read('plan.json');builds=read('qualification/builds.json');counts=collections.Counter();cells=[];files={};unresolved=[]
for c in p['cells']:
 row={'id':c['id'],'kind':c['kind'],'samples':{}}
 for side in ['A','B']:
  cfg=c[side];sel=cfg.get('selector',c['selector']);key='/'.join([c['target'],cfg['pin'],c['package']]);identity=json.dumps([sel,cfg['env'],c.get('catalog_id')],sort_keys=True)
  modes=builds.get(key,{});checks={m:v.get('configs',{}).get(identity,{'status':'UNBOUND'}) for m,v in modes.items()};ordinary=modes.get('ordinary',{});check=checks.get('ordinary',{})
  field='test_leaves' if c['kind']=='test-cycle' else 'benchmark_leaves';leaves=check.get(field,[])
  valid=set(checks)==MODES and bool(leaves) and all(v['status']=='PASS' and v.get(field)==leaves for v in checks.values())
  item={'qualification':'CORRECTNESS_PASS' if valid else 'FAILED_UNSUPPORTED_OR_UNBOUND','all3':{m:v['status'] for m,v in checks.items()},'expected_leaves':leaves,'qualification_key':key,'configuration_identity':identity}
  counts[item['qualification']]+=1
  if not valid:unresolved.append([c['id'],side,item['all3']])
  if ordinary.get('sha256'):
   binary=ordinary['binary'];assert sha(binary)==ordinary['sha256'];files[binary]=ordinary['sha256']
   parallel=c['id'].startswith(('graph-workers/','evacuation/','old-fill-region/'));tiny=c['target'].startswith('tinygo')
   env=dict(ordinary['build']['env']);env.update({'GOMAXPROCS':'8' if parallel else '1'});env.update(cfg['env'])
   argv=['/usr/bin/taskset','-c','0,2,4,6,8,10,12,14' if parallel else '0',binary,'-test.count=1','-test.v']+([] if tiny else ['-test.timeout=3m'])
   argv+=['-test.run='+sel] if c['kind']=='test-cycle' else ['-test.run=^$','-test.bench='+sel,'-test.benchtime=500ms']
   if 'catalog_id' in c:argv+=['-wago.corpus='+c['catalog_id']]
   item.update(argv=argv,cwd=check.get('receipt',{}).get('cwd'),env=env,binary_sha256=ordinary['sha256'],execution_authorized=False)
  row['samples'][side]=item
 a=row['samples']['A']['expected_leaves'];b=row['samples']['B']['expected_leaves']
 if c['kind']=='test-cycle':
  match=bool(a and b);join='whole process; side-specific pass leaf gates'
 elif c['id'].startswith('domains-topology/'):
  norm=lambda xs:[re.sub(r'shared=(true|false)','shared=CONTROLLED_AXIS',x) for x in xs]
  match=bool(a) and norm(a)==norm(b);join='approved same/separate Runtime axis; explicit leaf mapping'
 else:match=bool(a) and a==b;join='exact benchmark leaf identity'
 row.update(paired_leaf_match=match,metric_join=join)
 if not match:unresolved.append([c['id'],'PAIR_LEAF_MISMATCH'])
 cells.append(row)
rng=random.Random(p['seed']);ordered=[]
for prefix in ['focused','corpus/compile/','corpus/runtime/']:
 group=[r for r in cells if (not r['id'].startswith('corpus/') if prefix=='focused' else r['id'].startswith(prefix))];rng.shuffle(group);ordered+=group
assert len(cells)==349 and len({c['id'] for c in cells})==349
assert len(cells)*p['blocks']*4==8376
for path in (H/'.prepared').glob('*.test'):files[str(path)]=sha(path)
# Include every byte of each source/input/overlay export and all symlink identities.
links={}
for tree in (H/'.prepared').iterdir():
 if not tree.is_dir() or not (tree/'go.mod').exists():continue
 for path in sorted(tree.rglob('*')):
  if path.is_symlink():links[str(path)]=str(path.readlink())
  elif path.is_file():files[str(path)]=sha(path)
for path in sorted(H.rglob('*')):
 if '.prepared' in path.parts or '__pycache__' in path.parts or path.name in ['seal-manifest.json','summary.json']:continue
 if path.is_file():files[str(path)]=sha(path)
files['/usr/bin/taskset']=sha('/usr/bin/taskset')
for tool,digest in read('tool-manifest.json')['files'].items():
 assert sha(tool)==digest,tool
files.update(read('tool-manifest.json')['files']);links.update(read('tool-manifest.json')['symlinks'])
receipts=read('qualification/receipts.json');gates=read('gates/receipts.json');diag=read('detailed-census/receipts.json');sizes=read('shape-sizes/receipts.json')
failures=[{'family':family,'log':r['log'],'exit':r['exit']} for family,rows in [('qualification',receipts),('gates',gates),('diagnostic',diag),('shape',sizes)] for r in rows if r['exit']!=0]
summary={'status':'PREPARATION_ONLY','candidate_qualified':False,'pins':p['pins'],'cells':len(cells),'accepted_samples_if_all_qualified':8376,'side_qualification_counts':dict(counts),'unresolved_bindings':unresolved,'command_counts':{k:len(v) for k,v in [('qualification',receipts),('gates',gates),('diagnostic',diag),('shape',sizes)]},'failed_commands':failures,'benchmark_invocations':0,'timing_samples':0,'first_unresolved_external_risk':'Parent ARM64/QEMU failures unresolved; native Darwin/TinyGo audits remain parent-owned','performance_cost_and_benefit':'UNKNOWN; no timings taken','remaining_requirements':'remaining-requirements.json'}
write('summary.json',summary);files[str(H/'summary.json')]=sha(H/'summary.json')
write('seal-manifest.json',{'status':'PREPARATION_ONLY','candidate_valid':False,'acquisition_authorized':False,'reason':'Build/check binding seal only; all external gates and separate owner grant still required','plan_sha256':sha(H/'plan.json'),'source_pins':p['pins'],'cells':ordered,'qualification_counts':dict(counts),'unresolved_bindings':unresolved,'failed_commands':failures,'files':files,'symlinks':links,'global_owner_attestations':'NOT_GRANTED','requirements':read('remaining-requirements.json')})
print(json.dumps(summary,indent=2))
