from pathlib import Path
import importlib.util,json,subprocess,os
root=Path('/home/dev/Projects/dewdrop');tree=root/'.tmp/wago-gc-value-checks';out=root/'.tmp/wago-next-gc-perf/value-checks/admission'
spec=importlib.util.spec_from_file_location('measure',root/'docs/research/data/wago-local-perf-2026-10-20/measure.py');m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m);m.DATA=out
subprocess.run(['python3',str(out/'prepare.py'),str(tree)],check=True)
control=json.loads((root/'.tmp/wago-next-gc-perf/value-checks/micro/control-overlay.json').read_text())['Replace'];candidate=json.loads((out/'overlay.json').read_text())['Replace'];(out/'control-overlay.json').write_text(json.dumps({'Replace':dict(candidate,**control)},indent=2))
prior=list((m.WORK/'value-checks-candidate-1-home').rglob('*.wago'));assert len(prior)==1,prior
reports={}
for variant in ['control','candidate']:
 binary=out/(variant+'.test');overlay=out/('control-overlay.json' if variant=='control' else 'overlay.json')
 if variant!='control' or not binary.exists():m.measure('build-'+variant,['go','test','-p','2','-tags=wago_codegenstats','-c','-overlay',str(overlay),'-o',str(binary),'./src/wago'],cwd=tree,env=dict(m.ENV,GOMAXPROCS='2'),timeout=180)
 env=dict(m.ENV,GOMAXPROCS='2',WAGO_REAL_COMPILER_WASM=str(m.COMPILER),WAGO_REAL_COMPILER_STATS=str(out/(variant+'.json')),WAGO_ALLOW_ZERO_FUSIONS='1' if variant=='control' else '0')
 if variant=='candidate':env['WAGO_COMPARE_ARTIFACT']=str(prior[0])
 m.measure('run-'+variant,['taskset','-c','12,14',str(binary),'-test.run=^TestRealSharedGetterStaticAdmission$','-test.count=1','-test.v'],cwd=tree/'src/wago',env=env)
 reports[variant]=json.loads((out/(variant+'.json')).read_text())
s={k:{x:v[x] for x in ['compile_seconds','reused_resolutions','affected_functions','native_bytes','compile_resources','root_admission','native_sha256']} for k,v in reports.items()}
s['native_size_growth_percent']=100*(reports['candidate']['native_bytes']/reports['control']['native_bytes']-1)
s['final_matches_original_measured_candidate']='equal=true' in (out/'run-candidate.log').read_text()
(out/'summary.json').write_text(json.dumps(s,indent=2)+'\n');print(json.dumps(s,indent=2),flush=True)
