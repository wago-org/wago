#!/usr/bin/env python3
"""Untimed immutable-export adapter qualification. No benchmark/calibration calls."""
import hashlib,io,json,os,pathlib,re,subprocess,tarfile,signal
H=pathlib.Path(__file__).resolve().parent; R=H.parents[2]; O=H/'.prepared'; V=H/'qualification'; V.mkdir(exist_ok=True)
P=json.loads((H/'plan.json').read_text()); GO='/home/dev/.local/share/mise/installs/go/1.27.1/bin/go'; TG='/home/dev/.local/share/wago-tinygo-investigation/tinygo/bin/tinygo'
MODES=['ordinary','wagodebug','wago_regalloccheck']
def sha(p): return hashlib.sha256(pathlib.Path(p).read_bytes()).hexdigest()
def save(p,x):
 tmp=p.with_suffix(p.suffix+'.tmp');tmp.write_text(json.dumps(x,indent=2)+'\n');tmp.replace(p)
ENV={'PATH':str(pathlib.Path(GO).parent)+':/usr/bin:/bin','HOME':os.environ['HOME'],'GOROOT':str(pathlib.Path(GO).parents[1]),'GOTOOLCHAIN':'local','GOWORK':'off','GOMAXPROCS':'8','GOFLAGS':'-p=1','GOGC':'100','GOMEMLIMIT':'off','GODEBUG':'','GOEXPERIMENT':'','WAGO_SHARED_SCALAR':'1','XDG_CACHE_HOME':str(O/'cache')}
records=[];builds={}; bindings={}
def run(cmd,cwd,env,name):
 log=V/(name+'.log'); code=None
 with log.open('w') as f:
  proc=subprocess.Popen(cmd,cwd=cwd,env=env,stdout=f,stderr=subprocess.STDOUT,start_new_session=True)
  try:code=proc.wait(timeout=900)
  except subprocess.TimeoutExpired:os.killpg(proc.pid,signal.SIGKILL);proc.wait();code='TIMEOUT'
 raw=log.read_text(errors='replace'); passes=re.findall(r'^\s*--- PASS: (\S+)',raw,re.M)
 leaves=sorted(set(re.findall(r'FM_LEAF (\S+)',raw)))
 rec=dict(argv=cmd,cwd=str(cwd),env=env,exit=code,log=str(log.relative_to(H)),sha256=sha(log),passes=passes,leaves=leaves,skips=re.findall(r'^\s*--- SKIP: (\S+)',raw,re.M))
 records.append(rec);save(V/'receipts.json',records);print(name,code,len(passes),len(leaves),flush=True)
 return rec
for pin in ['baseline','port','shape-before','shape-after']:
 tree=O/pin
 if not tree.exists():
  tree.mkdir(); archive=subprocess.check_output(['git','archive',P['pins'][pin]],cwd=R)
  with tarfile.open(fileobj=io.BytesIO(archive)) as tf:tf.extractall(tree,filter='data')
 if pin in ['baseline','port']:
  (tree/'bench/suite/final_preparation_test.go').write_bytes((H/'fixtures/corpus_test.go.txt').read_bytes())
 if pin=='port':
  (tree/'src/core/runtime/gc/native/final_measurement_evacuation_test.go').write_bytes((H/'fixtures/evacuation_test.go.txt').read_bytes())
  (tree/'src/core/runtime/final_measurement_foreign_stack_test.go').write_bytes((H/'fixtures/foreign_stack_test.go.txt').read_bytes())
 for target,package,rel in [('native-linux-amd64',p,r) for p,r in ([('native','src/core/runtime/gc/native'),('guest','src/wago'),('bench','bench/suite')] if pin in ['baseline','port'] else [('guest','src/wago')])]+([('tinygo-0.42-patched-linux-amd64-threads-conservative',p,r) for p,r in [('native','src/core/runtime/gc/native'),('runtime','src/core/runtime')]] if pin=='port' else []):
  tiny=target.startswith('tinygo'); key='/'.join([target,pin,package]); builds[key]={}
  cells=[c for c in P['cells'] if c['package']==package and c['target']==target and any(c[s]['pin']==pin for s in ['A','B'])]
  configs={}
  for c in cells:
   for side in ['A','B']:
    cfg=c[side]
    if cfg['pin']!=pin:continue
    sel=cfg.get('selector',c['selector']); identity=json.dumps([sel,cfg['env'],c.get('catalog_id')],sort_keys=True)
    configs[identity]=(c,cfg,sel)
  for mode in MODES:
   binary=O/(pin+'-'+('tinygo-' if tiny else '')+mode+'-'+package+'.test')
   cmd=([TG,'test','-c','-scheduler=threads','-gc=conservative','-p=2'] if tiny else [GO,'test','-c'])+['-o',str(binary)]+([] if mode=='ordinary' else ['-tags='+mode])+['./suite' if package=='bench' else './'+rel]
   cwd=tree/'bench' if package=='bench' else tree
   build=run(cmd,cwd,ENV,pin+'-'+('tinygo-' if tiny else '')+mode+'-'+package+'-build')
   builds[key][mode]={'build':build,'binary':str(binary),'sha256':sha(binary) if binary.exists() else None,'configs':{}}
   save(V/'builds.json',builds)
   if build['exit']!=0:continue
   for i,(identity,(c,cfg,sel)) in enumerate(configs.items()):
    testsel=sel; expected=[]
    if c['kind']!='test-cycle':
     if package=='bench':testsel='^TestFinalPreparation'+('Compile' if c['kind']=='compile' else 'Runtime')+'$'
     elif 'RFWStage3Native' in sel:
      name=sel.split('/')[1].strip('^$');testsel='^TestRFWStage3MeasurementNative$/^'+cfg['env'].get('RFW_REGION','Off')+'$/^'+name+'$';expected=['BenchmarkRFWStage3Native/'+name]
     elif 'RFWStage3' in sel:
      name=sel.split('/')[1].strip('^$');testsel='^TestRFWStage3MeasurementGuest$/^'+name+'$/^'+str(cfg['env'].get('RFW_REGION')=='On').lower()+'$';expected=[sel.split('/')[0].strip('^$')+'/'+name]
     elif 'Graph' in sel:testsel=sel.replace('Benchmark','Test');expected=['BenchmarkFinalMeasurementGraph/'+sel.split('/')[1].strip('^$')]
     elif 'Evacuation' in sel:testsel=sel.replace('Benchmark','Test');expected=['BenchmarkFinalMeasurementEvacuation']
    # Canonical checked compile is always before any corpus execution configuration.
    if package=='bench' and c['kind']=='runtime':
     pre=run([str(binary),'-test.run=^TestFinalPreparationCompile$','-test.v','-test.count=1','-wago.corpus='+c['catalog_id']],tree/rel,ENV,pin+'-'+mode+'-preflight-'+c['catalog_id'])
     if pre['exit']!=0:
      builds[key][mode]['configs'][identity]={'status':'COMPILE_REJECTED','preflight':pre};save(V/'builds.json',builds);continue
    cmd=[str(binary),'-test.run='+testsel,'-test.v','-test.count=1']+([] if tiny else ['-test.timeout=10m'])
    if package=='bench':cmd+=['-wago.corpus='+c['catalog_id']]
    rec=run(cmd,tree/rel,ENV|cfg['env'],pin+'-'+('tinygo-' if tiny else '')+mode+'-'+package+'-'+str(i))
    # Parent-only PASS is not a leaf oracle: for a subselector require an actual slash leaf.
    passes=rec['passes']; actual=[n for n in passes if not any(x.startswith(n+'/') for x in passes)]
    nonempty=bool(actual) and ('/' not in testsel or any('/' in n for n in actual))
    leaves=rec['leaves'] if package=='bench' else expected
    ok=rec['exit']==0 and nonempty and not rec['skips'] and (c['kind']=='test-cycle' or bool(leaves))
    builds[key][mode]['configs'][identity]={'status':'PASS' if ok else 'FAILED_OR_EMPTY','receipt':rec,'test_leaves':actual,'benchmark_leaves':leaves}
    save(V/'builds.json',builds)
save(V/'builds.json',builds)
