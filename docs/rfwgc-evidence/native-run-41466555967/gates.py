#!/usr/bin/env python3
"""Offline gate/skip accounting; never executes candidate code."""
import collections,json,re,zipfile
from verify import HERE,SOURCE,DRIVER,BASE,REV,blob,sha

def save(n,x):(HERE/n).write_text(json.dumps(x,indent=2)+'\n')
spec=json.loads(blob(DRIVER,REV+'contracts.json'))
plan=json.loads(blob(DRIVER,BASE+'plan.json'))
prior=json.loads(blob('336416d2a006bbefded9e5b0b8539199bd6b0e3b',BASE+'plan.json'))
assert plan['lanes']==prior['lanes'] and len(plan['lanes'])==63
patterns={
'backend-focused':r'^(TestNativeImportVariantRoutesARM64|TestDynamicImportUnwindLocalJoinARM64|TestNativeLeafContextShortcutGCPolicyARM64|TestTable.*Lowering|TestBulkPhysicalArgumentSlots|TestTableSetHelperCallHintsArm64|TestWorkerModuleStatsComparisonARM64|TestConstFoldPackExecArm64|TestSameOperandCompareExecArm64)(/|$)',
'codegen-diagnostics':r'^(TestCodegenStatsPeepholesArm64|TestCompactShiftedAddImmediateArm64|TestConstFoldPackPeepArm64|TestEntryArgumentRegistersPromoteLeafLocalsArm64|TestEntryInitializedLocalSkipsZeroArm64|TestEntryParamPairsWrapperARM64|TestEntryZeroPairsARM64|TestIntervalRegionDynamicReuseArm64|TestLeafScratchPinAssignmentsArm64|TestZeroBranchIfArm64|TestSharedEstablishedPathPairs|TestSharedScalarLeafMatchesEstablishedCodeArm64)(/|$)',
'public-variants':r'^(TestNativeImportVariantSourceArtifactAndCalls|TestNativeImportVariantTrap|TestNativeImportVariantRespectsPolicies|TestNativeImportVariantForeignFallback)(/|$)',
'worker-synthetic':r'^TestCompileWorkers(DeterministicArm64|SizeSharedAdaptersDeterministicArm64)/synthetic',
'core3':r'^TestWasmtimeCore3Corpus(/|$)'}
report=[];skips=[];census=[]
for mode in ['wagodebug','wago_regalloccheck']:
 z=zipfile.ZipFile(HERE/'artifacts'/('darwin-ownership-'+mode+'-1.zip'));pre='receipts-'+mode+'/'
 overlay=json.loads(z.read(pre+'overlay.json'))['Replace']
 normalized={k.split('/source/')[1]:v.split('/harness/')[1] for k,v in overlay.items()}
 assert normalized=={k:REV+v for k,v in spec['darwin_test_overlays'].items()}
 rows=json.loads(z.read(pre+'results.json'));out={'mode':mode,'gates':{},'spec_totals':{},'core3_proof':'Parent exact 215/690 source assertions plus complete passing fixture census; successful child output is not echoed by broad parent.'}
 for row in rows:
  ident=row['id'];p=pre+ident+'/';cmd=json.loads(z.read(p+'command.json'));argv=json.loads(z.read(p+'effective-argv.json'))
  assert argv[:3]==['go','test','-mod=readonly'] and argv[3].startswith('-overlay=')
  assert argv[:2]+argv[4:]==cmd['argv']
  assert row['census_exit_code']==0
  ev=[]
  for l in z.read(p+'raw.jsonl').splitlines():
   try:ev.append(json.loads(l))
   except ValueError:pass
  term=[e for e in ev if e.get('Test') and e.get('Action') in ['pass','fail','skip']]
  if ident.startswith(('spec-v3-', 'spec-v3-signals-')):
   assert cmd['effective_environment']['WAGO_SPEC_REQUIRE_ZERO_GAPS']=='1'
   totals=[e['Output'] for e in ev if 'TOTAL[3.0]' in e.get('Output','')]
   assert len(totals)==1 and 'modules passed=2226 failed=0 skipped=0 | assertions passed=58238 failed=0 skipped=0' in totals[0]
   out['spec_totals'][ident]=totals[0]
  for gate,pat in patterns.items():
   if ((gate=='backend-focused' and ident=='backend-'+mode) or (gate in ['codegen-diagnostics','worker-synthetic'] and ident.startswith('diagnostics-default-')) or (gate in ['public-variants','core3'] and ident=='full-root-replay-'+mode)):
    selected=[{'test':e['Test'],'action':e['Action']} for e in term if re.search(pat,e['Test'])]
    assert selected and all(e['action']=='pass' for e in selected),(ident,gate)
    out['gates'][gate]={'lane':ident,'pass_events':len(selected),'events':selected}
  if row['failures']:
   assert row['exit_code']==1 and row['failure_events']==1
   assert {f['test'] for f in row['failures']}=={None}
  skips.append({'mode':mode,'lane':ident,'skip_events':row['skips']})
  raw=z.read(p+'package-census.jsonl').decode();raw=raw[raw.index('{'):];decoder=json.JSONDecoder();packages=[]
  while raw.strip():
   obj,end=decoder.raw_decode(raw.lstrip());raw=raw.lstrip()[end:];packages.append(obj)
  assert packages and not any(o.get('Error') or o.get('DepsErrors') for o in packages)
  selected=[o for o in packages if o.get('ImportPath')=='github.com/wago-org/wago/src/wago']
  if selected:
   fs=selected[0];files=fs.get('GoFiles',[])+fs.get('TestGoFiles',[])+fs.get('XTestGoFiles',[])
   if ident in ['full-root-replay-'+mode,'repaired-contracts-'+mode]:
    assert 'gc_conversion_store_host.go' in files and 'gc_conversion_store_other.go' not in files
    assert 'gc_extern_multi_table_test.go' in files
  census.append({'mode':mode,'lane':ident,'effective_argv':argv,'census_exit':row['census_exit_code'],'package_records':len(packages),'all_package_errors_absent':True})
 report.append(out)
assert 'total.modulesPassed != 215 || total.assertionsPassed != 690' in blob(SOURCE,'src/wago/wasmtime_core3_port_test.go').decode()
save('gate-accounting.json',{'all_63_broad_commands_identical_to_prior':True,'modes':report})
save('skip-accounting.json',skips);save('effective-command-census.json',census)
print(json.dumps([{ 'mode':r['mode'],'gates':{k:v['pass_events'] for k,v in r['gates'].items()}} for r in report]))
