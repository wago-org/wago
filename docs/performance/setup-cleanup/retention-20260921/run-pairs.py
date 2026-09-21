from pathlib import Path
import importlib.util,json,hashlib,sys,subprocess
root=Path(__file__).parent
name=sys.argv[1]
config=json.loads((root/(name+'-case.json')).read_text())
script=root/'wago/docs/performance/setup-cleanup/continuation/run-wasi-pairs.py'
spec=importlib.util.spec_from_file_location('paired_runner',script)
runner=importlib.util.module_from_spec(spec); spec.loader.exec_module(runner)
binaries={label:root/(name+'-'+label+'.test') for label in ['baseline','candidate']}
metadata={'binaries':{label:{'sha256':hashlib.sha256(path.read_bytes()).hexdigest()} for label,path in binaries.items()},'source':json.loads((root/'baseline.json').read_text()),'plan':json.loads((root/'plan.json').read_text()),'production':{},'benchmark':{}}
for label,files in config['production'].items():
 metadata['production'][label]={path:hashlib.sha256(Path(path).read_bytes()).hexdigest() for path in files}
for path in config['benchmarks_source']: metadata['benchmark'][path]=hashlib.sha256(Path(path).read_bytes()).hexdigest()
for label,path in binaries.items(): metadata['binaries'][label]['go_version_m']=subprocess.check_output(['go','version','-m',str(path)],text=True)
meta=root/(name+'-build.json'); meta.write_text(json.dumps(metadata,indent=2))
runner.CASES={f'{name}-{i}':(['-test.bench','/'.join('^'+part+'$' for part in leaf.split('/')),'-test.benchtime',config.get('benchtimes',{}).get(leaf,config.get('benchtime','100ms'))],[leaf]) for i,leaf in enumerate(config['leaves'])} if config.get('separate') else {name:(['-test.bench',config['pattern'],'-test.benchtime','100ms'],config['leaves'])}
sys.argv=[str(script),'--baseline',str(binaries['baseline']),'--candidate',str(binaries['candidate']),'--build-metadata',str(meta),'--output',str(root/config.get('output',name+'-pairs')),'--samples','20','--cases',*runner.CASES,'--timeout','60','--start-temp','55','--max-temp','75','--cool-seconds','1']
runner.main()
