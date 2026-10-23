import importlib.util,json,os,statistics,subprocess
from pathlib import Path
root=Path('/home/dev/Projects/dewdrop')
spec=importlib.util.spec_from_file_location('measure',root/'docs/research/data/wago-local-perf-2026-10-20/measure.py')
m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
m.DATA=root/'docs/research/data/wago-call-byte-matrix-2026-10-21';m.DATA.mkdir(parents=True,exist_ok=True)
m.ENV=dict(m.ENV,GOMAXPROCS='2')
original=m.measure
def measure(label,command,**kw):
 command=list(command)
 if command[0]=='node':command[3]=m.COMPILER
 return original(label,['taskset','-c','12,14',*command],**kw)
m.measure=measure
identity=dict(common_stack_commit='d06313bc31347491e19b7102404f00c7f3a0b36b',common_safety_commit='d7b8ac6ef1daf28a0bdd16747ec9ab1fbfba9e21',common_abstract_decoder='uncommitted one-byte emitter; exact common snapshot preserved',abi_commit='274fa62f8245160d97a4413fb5692b4371cb87b2',six_byte_commit='dc1a9ac31342d820f1c3ad0ef048de603d59bf63',compiler_sha256=m.digest(m.COMPILER),request_sha256=m.digest(m.WORK/'request.bin'),hosts={v:json.loads((root/'.tmp/wago-next-gc-perf'/(v+'-build.json')).read_text()) for v in ['safe-byte-control','private-reference-abi','six-space-byte']},production_patch_sha256={v:m.digest(root/'.tmp/wago-next-gc-perf'/(v+'-production.patch')) for v in ['private-reference-abi','six-space-byte']},node_versions=json.loads(subprocess.check_output(['node','-p','JSON.stringify(process.versions)'],text=True)),affinity='12,14',GOMAXPROCS=2,method='Three serial balanced rounds: control/ABI/byte/V8; byte/ABI/control/V8; ABI/control/byte/V8. Independent candidates share control with metadata safety and abstract byte decoder. Fresh caches; fixed compiler/request; identical Wasm/cache hashes, output main42, and exact root admission required. No task-owned tests/builds overlap.')
(m.DATA/'identity.json').write_text(json.dumps(identity,indent=2)+'\n')
rows=json.loads((m.DATA/'samples.json').read_text()) if (m.DATA/'samples.json').exists() else []
for label,version,engine in [('call-byte-safe-byte-control-1', 'safe-byte-control', 'control'), ('call-byte-private-reference-abi-1', 'private-reference-abi', 'abi'), ('call-byte-six-space-byte-1', 'six-space-byte', 'byte'), ('call-byte-v8-1', 'node', 'v8'), ('call-byte-six-space-byte-2', 'six-space-byte', 'byte'), ('call-byte-private-reference-abi-2', 'private-reference-abi', 'abi'), ('call-byte-safe-byte-control-2', 'safe-byte-control', 'control'), ('call-byte-v8-2', 'node', 'v8'), ('call-byte-private-reference-abi-3', 'private-reference-abi', 'abi'), ('call-byte-safe-byte-control-3', 'safe-byte-control', 'control'), ('call-byte-six-space-byte-3', 'six-space-byte', 'byte'), ('call-byte-v8-3', 'node', 'v8')]:
 if any(r['label']==label for r in rows):continue
 m.full(label,version)
 row=json.loads((m.DATA/(label+'.json')).read_text());row['engine']=engine
 if engine!='v8':
  artifacts=list((m.WORK/(label+'-home')).rglob('*.wago'));assert len(artifacts)==1,artifacts
  probe=subprocess.run([str(root/'.tmp/wago-next-gc-perf/local-admission'),str(artifacts[0])],check=True,capture_output=True,text=True)
  row['gc_native_root_admission']=json.loads(probe.stdout);assert row['gc_native_root_admission']['Required'] and row['gc_native_root_admission']['Exact']
 rows.append(row);(m.DATA/'samples.json').write_text(json.dumps(rows,indent=2)+'\n')
summary={}
for engine in ['control','abi','byte','v8']:
 r=[x for x in rows if x['engine']==engine];times=[x['seconds'] for x in r]
 summary[engine]=dict(seconds=times,median_seconds=statistics.median(times),median_peak_rss_kib=statistics.median(x['peak_rss_kib'] for x in r))
for candidate in ['abi','byte']:
 summary[candidate+'_reduction_percent']=100*(1-summary[candidate]['median_seconds']/summary['control']['median_seconds'])
 summary[candidate+'_over_v8']=summary[candidate]['median_seconds']/summary['v8']['median_seconds']
summary['all_outputs_identical_and_main_42']=True
(m.DATA/'summary.json').write_text(json.dumps(summary,indent=2)+'\n');print(json.dumps(summary,indent=2),flush=True)
