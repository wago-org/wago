import importlib.util,json,subprocess
from pathlib import Path
root=Path('/home/dev/Projects/dewdrop')
spec=importlib.util.spec_from_file_location('measure',root/'docs/research/data/wago-local-perf-2026-10-20/measure.py');m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
m.DATA=root/'docs/research/data/wago-gc-read-combined-2026-10-21';m.DATA.mkdir(exist_ok=False)
m.ENV=dict(m.ENV,GOMAXPROCS='2');original=m.measure
m.measure=lambda label,command,**kw:original(label,['taskset','-c','12,14',*command],**kw)
identity=dict(qualification_commit='676f015cbe377af9ca4ce1be96e13d7f5ca36d2f',control_commit='dc961197a73698e7bbf3583769f8979fe3072fef',compiler_sha256=m.digest(m.COMPILER),request_sha256=m.digest(m.WORK/'request.bin'),control_build=json.load(open(root/'.tmp/wago-cast-array-full/build.json')),candidate_build=json.load(open(root/'.tmp/wago-gc-read-followups-full/build.json')),method='One serial control/candidate integration pair after all tests/builds stop. CPU12,14; GOMAXPROCS2; fresh caches; fixed compiler/request; no V8 timing. Directional combined timing, not a new three-pair effect estimate.')
for version,key in [('cast-array-candidate','control_build'),('gc-read-followups-candidate','candidate_build')]:assert m.digest(m.WORK/(version+'-host'))==identity[key]['binary_sha256']
(m.DATA/'identity.json').write_text(json.dumps(identity,indent=2)+'\n')
rows=[]
for engine,version in [('control','cast-array-candidate'),('candidate','gc-read-followups-candidate')]:
 label='gc-read-combined-'+engine;m.full(label,version);row=json.load(open(m.DATA/(label+'.json')));row['engine']=engine
 artifacts=list((m.WORK/(label+'-home')).rglob('*.wago'));assert len(artifacts)==1
 p=subprocess.run([str(root/'.tmp/wago-next-gc-perf/local-admission'),str(artifacts[0])],check=True,capture_output=True,text=True);row['root_admission']=json.loads(p.stdout);assert row['root_admission']['Required'] and row['root_admission']['Exact']
 rows.append(row);(m.DATA/'samples.json').write_text(json.dumps(rows,indent=2)+'\n')
summary=dict(control_seconds=rows[0]['seconds'],candidate_seconds=rows[1]['seconds'],reduction_percent=100*(1-rows[1]['seconds']/rows[0]['seconds']),all_output_hashes_main42_exact_roots_pass=True,single_pair_directional=True)
(m.DATA/'summary.json').write_text(json.dumps(summary,indent=2)+'\n');print(json.dumps(summary,indent=2),flush=True)
