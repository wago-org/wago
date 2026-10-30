#!/usr/bin/env python3
import pathlib,json,collections,re,statistics,math
OUT=pathlib.Path(__file__).resolve().parent
manifest=json.loads((OUT/'manifest.json').read_text());rows=[]
inv={}
if (OUT/'inventory.txt').exists():
 for line in (OUT/'inventory.txt').read_text().splitlines():
  if line.startswith('CORPUS_INVENTORY '):
   d=json.loads(line[17:]);inv[d['id']]=d

def bench_rows(text):
 out=[]
 for line in text.splitlines():
  parts=line.split()
  if not parts or not re.match(r'Benchmark(?:Exec|CommandExec)/',parts[0]):continue
  if 'ns/op' not in parts:continue
  out.append({'benchmark':parts[0],'iterations':int(parts[1]),'ns_per_op':float(parts[parts.index('ns/op')-1]),'raw':line})
 return out

def compiled(id):
 p=OUT/('static-'+id+'.txt');text=p.read_text();data=[json.loads(x[13:]) for x in text.splitlines() if x.startswith('CORPUS_STATS ')]
 assert len(data)==1,id
 d=data[0];r=d['Report'];fs=r['Functions']
 return {'compile_pass':'\nPASS\n' in text,'functions':d['Functions'],'diagnostic_functions':len(fs),'omitted_functions':r['Omitted'],'diagnostic_unknown_denominator_functions':sum(not f['DenominatorKnown'] for f in fs),'diagnostic_site_lower_bound':sum(f['Sites'] for f in fs),'proven_sites':sum(f['Sites'] for f in fs if f['Candidate']),'lowered_sites':d['Lowered'],'reasons':dict(collections.Counter(f['Reason'] for f in fs))}

telemetry=[]
if (OUT/'during-timing.jsonl').exists():telemetry=[json.loads(x) for x in (OUT/'during-timing.jsonl').read_text().splitlines()]
for m in manifest['benchmarks']:
 id=m['id'];r={'id':id,'artifact':m['artifact'],'artifact_sha256':m['actual_sha256'],'hash_matches':m['hash_matches'],'workload_kind':'command' if m.get('command') else 'export' if m.get('exec') else 'semantic','unit':'fresh command instance including host/IO and teardown; compilation excluded' if m.get('command') else 'one prepared invoke (complete vector case-set for semantic vectors); compile/instantiate excluded','static':compiled(id),'inventory':inv.get(id),'validation':{},'accepted_samples':{},'runtime_comparison':{}}
 for rev in ['baseline','stage2','baselineOff']:
  r['validation'][rev]={}
  for mode in ['ordinary','wagodebug','wago_regalloccheck']:
   p=OUT/f'validate-{rev}-{mode}-{id}.txt'
   if p.exists():
    text=p.read_text();r['validation'][rev][mode]={'pass':text.rstrip().endswith('PASS'),'rows':bench_rows(text),'option':'off' if rev=='baselineOff' else 'on'}
 samples={}
 for rev in ['baseline','stage2']:
  accepted=[]
  for t in telemetry:
   if not t['cell'].startswith(id+'-') or not t['cell'].endswith('-'+rev) or not t['accepted']:continue
   p=OUT/(t['cell']+f"-attempt{t['attempt']}.txt");accepted.extend([b|{'cell':t['cell'],'repeat':int(t['cell'].rsplit('-',2)[1])} for b in bench_rows(p.read_text())])
  r['accepted_samples'][rev]=accepted
  samples[rev]=collections.defaultdict(list)
  for b in accepted:samples[rev][b['benchmark']].append(b['ns_per_op'])
 for name in sorted(set(samples['baseline'])|set(samples['stage2'])):
  a=samples['baseline'][name];b=samples['stage2'][name]
  r['runtime_comparison'][name]={'baseline_accepted':len(a),'stage2_accepted':len(b),'baseline_median_ns':statistics.median(a) if a else None,'stage2_median_ns':statistics.median(b) if b else None,'delta_pct':(statistics.median(b)/statistics.median(a)-1)*100 if len(a)==len(b)==6 else None}
 for name,cell in r['runtime_comparison'].items():
  cell['ratio_stage2_over_baseline']=cell['stage2_median_ns']/cell['baseline_median_ns'] if cell['baseline_accepted']==cell['stage2_accepted']==6 else None
 ratios=[c['ratio_stage2_over_baseline'] for c in r['runtime_comparison'].values() if c['ratio_stage2_over_baseline'] is not None]
 r['workload_ratio']=math.exp(sum(map(math.log,ratios))/len(ratios)) if ratios and len(ratios)==len(r['runtime_comparison']) else None
 # Zero removals is an implication of zero actual lowerings, NOT a dynamic counter observation.
 r['dynamic_removals']={'native_gc_allocation_materializations_removed':0 if r['static']['lowered_sites']==0 else None,'mechanism':'static exclusion: no emitted consumer lowerings; exhaustive constructor inventory corroborates if complete','guest_allocation_charge_counters_measured':False,'guest_linear_memory_malloc_counts_measured':False,'host_go_allocs_are_guest_allocations':False}
 rows.append(r)

unique={}
for r in rows:unique.setdefault(r['artifact_sha256'],r)
keys=['functions','diagnostic_functions','omitted_functions','diagnostic_unknown_denominator_functions','diagnostic_site_lower_bound','proven_sites','lowered_sites']
totals=lambda rs:{k:sum(r['static'][k] for r in rs) for k in keys}
summary={'catalog_sha256':manifest['catalog_sha256'],'entries':len(rows),'unique_artifact_hashes':len(unique),'checks':len(manifest['checks']),'workload_kinds':dict(collections.Counter(r['workload_kind'] for r in rows)),'present_and_pinned':sum(r['hash_matches'] for r in rows),'unbuildable':0,'compile_passes':sum(r['static']['compile_pass'] for r in rows),'catalog_weighted_static':totals(rows),'unique_hash_static':totals(unique.values()),'complete_constructor_inventories':len(inv),'constructor_sites':sum(sum(i['constructors'].values()) for i in inv.values()),'validation_passes':{rev:{mode:sum(r['validation'].get(rev,{}).get(mode,{}).get('pass',False) for r in rows) for mode in ['ordinary','wagodebug','wago_regalloccheck']} for rev in ['baseline','stage2','baselineOff']},'accepted_invocations':sum(t['accepted'] for t in telemetry),'rejected_invocations':sum(not t['accepted'] for t in telemetry),'modules_with_six_pairs':sum(bool(r['runtime_comparison']) and all(c['baseline_accepted']==c['stage2_accepted']==6 for c in r['runtime_comparison'].values()) for r in rows),'timing_blocker':(OUT/'timing-blocker.txt').read_text().strip() if (OUT/'timing-blocker.txt').exists() else None}
summary['runtime_module_gaps']=len(rows)-summary['modules_with_six_pairs']
def aggregate(rs):
 vals=[r['workload_ratio'] for r in rs if r['workload_ratio'] is not None]
 return {'complete_workloads':len(vals),'expected_workloads':len(rs),'geomean_stage2_over_baseline':math.exp(sum(map(math.log,vals))/len(vals)) if vals else None,'complete':len(vals)==len(rs)}
summary['runtime_aggregates']={'method':'Each row ratio is Stage2 median of six accepted samples / baseline median of six; geometric mean of rows within each catalog workload, then geometric mean across workloads. No zero-elimination runtime winner claim. Incomplete coverage is not a full-corpus result.','all_workloads_weighted_once':aggregate(rows),'commands':aggregate([r for r in rows if r['workload_kind']=='command']),'exports_and_semantic':aggregate([r for r in rows if r['workload_kind']!='command'])}
raw=[c['ratio_stage2_over_baseline'] for r in rows for c in r['runtime_comparison'].values() if c['ratio_stage2_over_baseline'] is not None]
summary['runtime_aggregates']['raw_row_weighted_labelled']={'expected_rows':135,'complete_rows':len(raw),'geomean':math.exp(sum(map(math.log,raw))/len(raw)) if raw else None}
(OUT/'per-module.json').write_text(json.dumps(rows,indent=2)+'\n');(OUT/'summary.json').write_text(json.dumps(summary,indent=2)+'\n')
lines=['| Catalog entry | Kind / timed unit | Compile | Proven / lowered sites | BaselineOn / Stage2On checks (three modes) | Six paired runtime cells |','|---|---|---|---|---|---|']
for r in rows:
 checks=[''.join('P' if r['validation'].get(rev,{}).get(mode,{}).get('pass') else '?' for mode in ['ordinary','wagodebug','wago_regalloccheck']) for rev in ['baseline','stage2']]
 complete=bool(r['runtime_comparison']) and all(c['delta_pct'] is not None for c in r['runtime_comparison'].values())
 lines.append(f"| {r['id']} | {r['workload_kind']} | {'pass' if r['static']['compile_pass'] else 'FAIL'} | {r['static']['proven_sites']} / {r['static']['lowered_sites']} | {' / '.join(checks)} | {'complete' if complete else 'GAP'} |")
(OUT/'tables.md').write_text('\n'.join(lines)+'\n')
print(json.dumps(summary,indent=2))

rlines=['| Workload | Runtime row (timed unit in per-module manifest) | BaselineOn samples / median ns | Stage2On samples / median ns | Stage2 / baseline |','|---|---|---|---|---|']
for r in rows:
 expected=r['validation']['stage2']['ordinary']['rows']
 for b in expected:
  c=r['runtime_comparison'].get(b['benchmark'],{})
  ratio=c.get('ratio_stage2_over_baseline')
  rlines.append(f"| {r['id']} | {b['benchmark']} | {c.get('baseline_accepted',0)} / {c.get('baseline_median_ns','GAP')} | {c.get('stage2_accepted',0)} / {c.get('stage2_median_ns','GAP')} | {format(ratio,'.6f') if ratio is not None else 'GAP'} |")
(OUT/'runtime-rows.md').write_text('\n'.join(rlines)+'\n')
