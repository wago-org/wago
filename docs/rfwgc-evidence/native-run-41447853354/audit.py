#!/usr/bin/env python3
"""Read-only pinned source and archived-event audit; no test execution."""
import collections,hashlib,json,pathlib,re,subprocess,zipfile
P=pathlib.Path(__file__).resolve().parent
ROOT=P.parents[2]
SOURCE='7e41fafbbcd737ed1edce11b94b43b4eda134dcf'
def git(*args,cwd=ROOT):return subprocess.check_output(['git',*args],cwd=cwd)
def save(name,obj):(P/name).write_text(json.dumps(obj,indent=2)+'\n')
def sha(b):return hashlib.sha256(b).hexdigest()
paths=['src/wago/gc_extern_multi_table_test.go','src/wago/gc_conversion_store_host.go','src/wago/externref_global_conversion_test.go','src/wago/gc_conversion_store_other.go','src/wago/globals.go','src/wago/reference_store.go','src/wago/wasmtime_core3_port_test.go','src/wago/spectest_exec_test.go','src/wago/native_import_variant_arm64_test.go','src/wago/native_import_variant_safety_arm64_test.go','src/core/compiler/backend/railshot/arm64/dynamic_import_join_test.go','src/core/compiler/backend/railshot/arm64/compile.go','src/core/compiler/backend/railshot/arm64/call.go','cli/wago-installer/go.mod','cli/wago-installer/go.sum','tests/corpus/regressions/wasmtime-core3/core/gc/const-expr-gc/source.wast']
snap={}
for n in paths:
 proc=subprocess.run(['git','show',SOURCE+':'+n],cwd=ROOT,capture_output=True)
 if proc.returncode: snap[n]={'absent':True,'git_error':proc.stderr.decode()};continue
 b=proc.stdout;snap[n]={'sha256':sha(b),'text':b.decode()}
save('pinned-source-audit.json',snap)
old=json.loads((P.parent/'native-run-41396042416/sibling-audit.json').read_text());siblings=[]
for row in old:
 root=pathlib.Path(row['root']);files={}
 for n in row['files']:
  b=(root/n).read_bytes();files[n]={'sha256':sha(b),'same_as_prior_audit':sha(b)==row['files'][n],'relevant_lines':[{'line':i,'text':s} for i,s in enumerate(b.decode().splitlines(),1) if re.search('extern|owner|epoch|generation|resource|ExternConvertAny',s)][:100]}
 patterns='opaque converted publication|setConversionGlobalPublic|initializeConversionReceipts|direct Go import local-state join differs'
 proc=subprocess.run(['rg','-n',patterns]+[d for d in ['src','scripts'] if (root/d).is_dir()],cwd=root,capture_output=True,text=True)
 siblings.append({'root':str(root),'head':git('rev-parse','HEAD',cwd=root).decode().strip(),'tracked_status':git('status','--short','--untracked-files=no',cwd=root).decode(),'files':files,'analogous_implementation_search':{'pattern':patterns,'exit':proc.returncode,'stdout':proc.stdout,'stderr':proc.stderr}})
save('sibling-audit.json',siblings)
closure=[];spec=[];census=[]
selectors=re.compile(r'TestWasmtimeCore3Corpus|TestConverted|TestNativeImportVariant|TestGCExternConversionAcrossReferenceTables|TestDynamicImportUnwindLocalJoinARM64|TestNativeLeafContextShortcutGCPolicyARM64|TestExample|TestLocalExternrefGlobalsRespectFeatureStoreAndLifetimeBoundaries|TestTable(GetSet|Init|Fill|Grow|Copy)Lowering|TestBulkPhysicalArgumentSlots|TestTableSetHelperCallHintsArm64|Test.*Worker|TestSharedEntryGuardCancellationAndGC')
for f in sorted((P/'artifacts').glob('darwin*.zip')):
 mode=f.name.split('-')[2];z=zipfile.ZipFile(f);matches=[];join_errors=[];spec_lines=[]
 for n in z.namelist():
  if not n.endswith('/raw.jsonl'):continue
  for l in z.read(n).splitlines():
   try:e=json.loads(l)
   except ValueError:continue
   if e.get('Action') in ('pass','fail','skip') and selectors.search(e.get('Test','')):
    matches.append({'member':n,'action':e['Action'],'package':e.get('Package'),'test':e['Test']})
   if 'direct Go import local-state join differs' in e.get('Output',''):join_errors.append({'member':n,'event':e})
   if '/spec-v3' in n and re.search(r'gc/ref_test.wast:(44|102|103|108|109) |TOTAL\[3.0\]',e.get('Output','')):spec_lines.append({'member':n,'output':e['Output']})
 closure.append({'mode':mode,'exact_terminal_events':matches,'old_join_diagnostic_occurrences':join_errors})
 spec.append({'mode':mode,'observed_assertion_diagnostics':spec_lines})
 for r in json.loads(z.read(f'receipts-{mode}/results.json')):
  lane=r['id'];cmd=json.loads(z.read(f'receipts-{mode}/{lane}/command.json'))
  census.append({'mode':mode,'lane':lane,'argv':cmd['argv'],'cwd':cmd['cwd'],'environment':cmd['environment'],'effective_environment':cmd['effective_environment'],'status':r['status'],'exit_code':r['exit_code'],'census_exit_code':r['census_exit_code'],'passing_test_events':r['passing_test_events'],'failure_events':r['failure_events'],'skip_events':len(r['skips'])})
save('closure-events.json',closure);save('spec-v3-diagnostics.json',spec);save('command-census.json',census)
patterns=json.loads((P.parent/'native-run-41396042416/log-safety.json').read_text())['patterns'];hits=[]
for f in [P/'full-job-logs.zip',*sorted((P/'artifacts').glob('*.zip'))]:
 with zipfile.ZipFile(f) as z:
  for n in z.namelist():
   if n.endswith('.wasm'):continue
   b=z.read(n)
   for pat in patterns:
    if re.search(pat.encode(),b):hits.append({'archive':f.name,'member':n,'pattern':pat})
save('log-safety.json',{'patterns':patterns,'matches':hits,'limitation':'Known-pattern scan only; existing GitHub masking preserved.'})
assert not hits
print('Audit receipts written; no tests executed.')
