#!/usr/bin/env python3
"""Source and historical receipt comparison only: no Go execution/builds."""
import collections,gzip,hashlib,json,re,subprocess
from pathlib import Path
P=Path(__file__).resolve().parent
ROOT=Path.cwd(); OLD='5b02ec67afe0dce181a7f8d508e270c7ecb0ecfc'; NEW='08f511fac41a77f4c0f9046f88ebc78ad05a5da6'
Q=ROOT/'docs/rfwgc-evidence/final-combined-5b02'
def git(*args):return subprocess.check_output(['git',*args])
def blob(pin,p):return git('show',pin+':'+p)
def sha(b):return hashlib.sha256(b).hexdigest()
def save(n,v):(P/n).write_text(json.dumps(v,indent=2)+'\n')
def tree(pin):
 d={}
 for row in git('ls-tree','-rz',pin).split(b'\0'):
  if row:
   h,p=row.split(b'\t');d[p.decode()]=h.decode()
 return d
a,b=tree(OLD),tree(NEW)
changed=[p for p in sorted(a.keys()|b.keys()) if a.get(p)!=b.get(p)]
non_docs=[p for p in changed if not p.startswith('docs/')]
assert non_docs==['.github/workflows/ci.yml','src/core/runtime/engine_int_tinygo_test.go','src/wago/gc_extern_multi_table_test.go','src/wago/rr_recheck_flags_current_test.go','tests/integration/cipolicy/workflows_test.go']
# Stronger than selected-file identity: every production file in src is identical.
production={p:a[p] for p in a.keys()|b.keys() if p.startswith('src/') and not p.endswith('_test.go')}
assert all(a.get(p)==b.get(p) for p in production)
(P/'inputs/non-doc-source-delta.patch').write_bytes(git('diff',OLD,NEW,'--',*non_docs))
paths=['src/wago/rr_recheck_adversary_test.go','src/wago/shared_entry_guard_arm64_test.go','src/wago/rr_recheck_flags_current_test.go','src/wago/gc_conversion_store_host.go','src/wago/gc_conversion_store_other.go','src/wago/gc_extern_multi_table_test.go','src/core/runtime/engine_int_tinygo_test.go']
identities={}
for p in paths:
 x,y=blob(OLD,p),blob(NEW,p)
 identities[p]={'old_sha256':sha(x),'new_sha256':sha(y),'byte_equal':x==y,'build_constraint':next((l for l in y.decode().splitlines() if l.startswith('//go:build')),'')}
 (P/'inputs'/Path(p).name).write_bytes(y)
 if p.endswith(('rr_recheck_adversary_test.go','shared_entry_guard_arm64_test.go','gc_conversion_store_host.go','gc_conversion_store_other.go')):assert x==y
 if p.endswith('rr_recheck_flags_current_test.go'):
  assert re.sub(rb'\s+',b'',x)==re.sub(rb'\s+',b'',y)
  identities[p]['all_nonwhitespace_bytes_equal']=True
save('source-comparison.json',{'base':OLD,'native_source':NEW,'changed_non_docs':non_docs,'all_src_production_entries_identical':len(production),'production_entries':production,'relevant_blob_identities':identities})
# Retain exact historical commands/exits and independently parse plain Go events.
results=json.loads((Q/'qemu-repairs/results.json').read_text())
audit=json.loads((P/'audit.json').read_text())
comparison={}
for mode in ['ordinary','wagodebug','wago_regalloccheck']:
 raw=gzip.decompress((Q/f'qemu-repairs/arm64-{mode}-guest.log.gz').read_bytes());s=raw.decode()
 starts=re.findall(r'^=== RUN\s+(\S+)',s,re.M)
 passes=re.findall(r'^\s*--- PASS: (\S+) ',s,re.M)
 fails=re.findall(r'^\s*--- FAIL: (\S+) ',s,re.M)
 skips=re.findall(r'^\s*--- SKIP: (\S+) ',s,re.M)
 assert len(starts)==len(set(starts)) and set(starts)==set(passes)|set(fails)|set(skips)
 expected=['TestRRRecheckStorageOnlyReturnBehindDynamicReader','TestRRRecheckStorageOnlyReturnBehindDynamicReader/none','TestRRRecheckStorageOnlyReturnBehindDynamicReader/table','TestSharedEntryGuardCancellationAndGC']
 assert set(fails)==set(expected) and not skips
 native={}
 for gate in ['wago-focused','wago-guard','wago-race-focused','wago-race-guard']:
  events=[json.loads(l) for l in (P/mode/(gate+'.log')).read_text().splitlines()]
  native[gate]={e['Test']:e['Action'].upper() for e in events if e.get('Test') and e['Action'] in ['pass','fail','skip']}
 normal=native['wago-focused']|native['wago-guard'];race=native['wago-race-focused']|native['wago-race-guard']
 assert all(normal[n]==race[n]=='PASS' for n in expected)
 top={n for n in starts if '/' not in n}
 cmd=next(r for r in results if r['label']==f'arm64-{mode}-guest')
 assert cmd['exit']==1
 output=[l for l in s.splitlines() if 'secondAdmissionErr=' in l or 'second storage-only call stalled:' in l or 'native loop did not cancel' in l]
 native_output={g:[e['Output'].rstrip() for e in map(json.loads,(P/mode/(g+'.log')).read_text().splitlines()) if 'secondAdmissionErr=' in e.get('Output','')] for g in ['wago-focused','wago-race-focused']}
 assert all(len(lines)==2 and all('storageReturnBlocked=false secondAdmissionErr=<nil>' in l for l in lines) for lines in native_output.values())
 comparison[mode]={'qemu_status':'FAIL','qemu_raw_sha256':sha(raw),'qemu_command':cmd['command'],'qemu_exit':cmd['exit'],'qemu_build':next(r for r in results if r['label']==f'arm64-{mode}-guest-build'),'qemu_top_level_started':len(top),'qemu_event_census':{'run':len(starts),'pass':len(passes),'fail':len(fails),'skip':len(skips)},'qemu_failures':fails,'qemu_failure_output':output,'native_failed_assertion_mapping':{n:{'qemu':'FAIL','native':'PASS','native_race':'PASS'} for n in expected},'native_storage_output':native_output,'qemu_tests_absent_from_native':sorted(top-normal.keys()),'qemu_subtests_absent_from_native':sorted(set(starts)-normal.keys()),'native_tests_absent_from_qemu':sorted({n for n in normal if '/' not in n}-top),'native_gate_commands':{r['label']:r for r in json.loads((P/mode/'commands.json').read_text()) if r['label'] in native},'full_qemu_gate_equivalent':False}
save('qemu-native-comparison.json',comparison)
print(json.dumps({m:{k:v[k] for k in ['qemu_event_census','qemu_top_level_started','qemu_tests_absent_from_native','qemu_subtests_absent_from_native']} for m,v in comparison.items()},indent=2))
# Independently inspect actual historical export, without trusting worker verdict.
export=Path(json.loads((Q/'qemu-repairs/source.json').read_text())['export'])
probe_checks={}
for path in json.loads((P/'inputs/plan.json').read_text())['probe_sha256']:
 before=blob(OLD,path);after=blob(NEW,path);actual=(export/path).read_bytes()
 assert actual==before
 if path.endswith('rr_recheck_flags_current_test.go'):assert re.sub(rb'\s+',b'',before)==re.sub(rb'\s+',b'',after)
 else:assert before==after
 probe_checks[path]={'export_sha256':sha(actual),'old_blob_sha256':sha(before),'native_blob_sha256':sha(after),'unchanged_except_flag_helper_whitespace':True}
# Hash every historical production source against exact 5b Git object identity.
export_production={}
for path,header in production.items():
 data=(export/path).read_bytes();oid=hashlib.sha1(b'blob '+str(len(data)).encode()+b'\0'+data).hexdigest()
 assert oid==header.split()[2]
 export_production[path]=sha(data)
# The native host implementation returns true for the fixture's externref table.
mt=blob(NEW,'src/wago/gc_extern_multi_table_test.go').decode()
hexstr=re.search(r'const gcExternMultiTableWasm\s*=\s*"([0-9a-f]+)"',mt).group(1)
wasm=bytes.fromhex(hexstr)
def uleb(buf,i):
 n=s=0
 while True:
  c=buf[i];i+=1;n|=(c&127)<<s
  if c<128:return n,i
  s+=7
pos=8;tables=[]
while pos<len(wasm):
 sec=wasm[pos];size,begin=uleb(wasm,pos+1);end=begin+size
 if sec==4:
  count,i=uleb(wasm,begin)
  for _ in range(count):
   typ=wasm[i];i+=1;flags,i=uleb(wasm,i);minimum,i=uleb(wasm,i)
   if flags&1:maximum,i=uleb(wasm,i)
   tables.append(typ)
 pos=end
assert tables[0]==0x6f
save('linux-selection-proof.json',{'source':NEW,'target':'linux/arm64','modes':['ordinary','wagodebug','wago_regalloccheck'],'probe_export_checks':probe_checks,'historical_export_production_sha256':export_production,'production_export_entries_verified':len(export_production),'selected_host_hooks':'src/wago/gc_conversion_store_host.go','excluded_hooks':'src/wago/gc_conversion_store_other.go','excluded_tinygo_test':'src/core/runtime/engine_int_tinygo_test.go','multitable_fixture_table_types':tables,'multitable_host_hook_true':True,'multitable_branch':'positive on Linux ARM64 in both pins; unchanged assertions and wasm','rr_adapter':'identical non-whitespace bytes; same field and return expression','limitations':'Static selection analysis for relevant changed files; exact native compiler/test receipts in audit.json. No build/list/test executed by this audit.'})
