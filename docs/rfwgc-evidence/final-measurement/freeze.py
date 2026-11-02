#!/usr/bin/env python3
"""Freeze historically qualified commands without granting candidate validity."""
import hashlib,json,pathlib,random,collections,os,subprocess
H=pathlib.Path(__file__).resolve().parent

def sha(p):return hashlib.sha256(pathlib.Path(p).read_bytes()).hexdigest()
def main():
 p=json.loads((H/'plan.json').read_text());b=json.loads((H/'qualification/builds.json').read_text());status=json.loads((H/'candidate-status.json').read_text())
 cells=[];counts=collections.Counter();manifest={}
 for c in p['cells']:
  row={'id':c['id'],'kind':c['kind'],'samples':{}}
  for s in ['A','B']:
   cfg=c[s];sel=cfg.get('selector',c['selector']);key='/'.join([c['target'],cfg['pin'],c['package']]);identity=json.dumps([sel,cfg['env'],c.get('catalog_id')],sort_keys=True)
   modes=b.get(key,{});checks={m:v.get('configs',{}).get(identity,{'status':'UNBOUND'}) for m,v in modes.items()};ordinary=modes.get('ordinary',{});ordinary_check=checks.get('ordinary',{})
   valid=set(checks)=={'ordinary','wagodebug','wago_regalloccheck'} and all(v['status']=='PASS' for v in checks.values())
   leaves=ordinary_check.get('test_leaves' if c['kind']=='test-cycle' else 'benchmark_leaves',[])
   valid=valid and bool(leaves) and all(v.get('test_leaves' if c['kind']=='test-cycle' else 'benchmark_leaves')==leaves for v in checks.values())
   counts['QUALIFIED_HISTORICAL' if valid else 'FAILED_UNSUPPORTED_OR_UNBOUND']+=1
   item={'qualification':'QUALIFIED_HISTORICAL' if valid else 'FAILED_UNSUPPORTED_OR_UNBOUND','all3':{m:v['status'] for m,v in checks.items()},'expected_leaves':leaves}
   if ordinary.get('sha256'):
    binary=ordinary['binary'];assert sha(binary)==ordinary['sha256'];manifest[binary]=sha(binary)
    parallel=c['id'].startswith(('graph-workers/','evacuation/'));tiny=c['target'].startswith('tinygo')
    env=dict(ordinary['build']['env']);env.update({'GOMAXPROCS':'8' if parallel else '1'});env.update(cfg['env'])
    cmd=['/usr/bin/taskset','-c','0,2,4,6,8,10,12,14' if parallel else '0',binary,'-test.count=1','-test.v']+([] if tiny else ['-test.timeout=3m'])
    cmd+=['-test.run='+sel] if c['kind']=='test-cycle' else ['-test.run=^$','-test.bench='+sel,'-test.benchtime=500ms']
    if 'catalog_id' in c:cmd+=['-wago.corpus='+c['catalog_id']]
    item.update(argv=cmd,cwd=ordinary_check.get('receipt',{}).get('cwd',str(H/'.prepared'/cfg['pin'])),env=env,binary_sha256=sha(binary))
   row['samples'][s]=item
  row['paired_leaf_match']=(bool(row['samples']['A']['expected_leaves']) and bool(row['samples']['B']['expected_leaves'])) if c['kind']=='test-cycle' else (row['samples']['A']['expected_leaves']==row['samples']['B']['expected_leaves'] and bool(row['samples']['A']['expected_leaves']))
  row['metric_join']='complete-test-cycle; pass leaf names are side-specific gates' if c['kind']=='test-cycle' else 'exact benchmark leaf name'
  cells.append(row)
 rng=random.Random(p['seed']);ordered=[]
 for prefix in ['focused','corpus/compile/','corpus/runtime/']:
  group=[r for r in cells if (not r['id'].startswith('corpus/') if prefix=='focused' else r['id'].startswith(prefix))];rng.shuffle(group);ordered+=group
 for path in (H/'.prepared').glob('*.test'):manifest[str(path)]=sha(path)
 for path in [H/'plan.json',H/'candidate-status.json',H/'qualification/builds.json',H/'qualification/receipts.json',H/'input-census.json',H/'census.json',H/'host-readonly.json',H/'throttle-visibility.json',H/'source-relation.json',H/'continuation-summary.json',H/'allocation-byte-census.json',H/'census-fields.json',H/'preparation-drain.json',H/'exports-final-integrity.json',H/'required-additions.json',H/'additions-v3/receipts.json',H/'detailed-census/receipts.json',H/'shape-sizes/receipts.json',*sorted((H/'fixtures').glob('*')),*sorted(H.glob('*.py'))]:manifest[str(path)]=sha(path)
 for path in ["/home/dev/.local/share/mise/installs/go/1.27.1/bin/go","/home/dev/.local/share/wago-tinygo-investigation/tinygo/bin/tinygo","/usr/bin/taskset"]:manifest[path]=sha(path)
 # Seal actual exported inputs and tool trees, not just metadata declarations.
 for tree in (H/'.prepared').glob('*'):
  if tree.is_dir() and (tree/'go.mod').exists():
   for name in ['go.mod','go.sum','go.work','go.work.sum']:
    path=tree/name
    if path.is_file():manifest[str(path)]=sha(path)
   for root in ['corpus','tests','src','bench']:
    for path in sorted((tree/root).rglob('*')):
     if path.is_file():manifest[str(path)]=sha(path)
 for toolroot in [pathlib.Path('/home/dev/.local/share/mise/installs/go/1.27.1'),pathlib.Path('/home/dev/.local/share/wago-tinygo-investigation/tinygo')]:
  for path in sorted(toolroot.rglob('*')):
   if path.is_file() and not path.is_symlink():manifest[str(path)]=sha(path)
 out={'status':status['status'],'reason':'DR-1/DR-2/DR-3 repair and new source qualification required; this is NOT a final candidate seal','plan_sha256':sha(H/'plan.json'),'source_pins':p['pins'],'qualification_counts':dict(counts),'cells':ordered,'files':manifest,'global_owner_attestations':'NOT_GRANTED','candidate_valid':False}
 (H/'expanded-historical.json').write_text(json.dumps(out,indent=2)+'\n')
 print(json.dumps({'status':out['status'],'counts':dict(counts),'cells':len(cells),'manifest_files':len(manifest)}))
if __name__=='__main__':main()
