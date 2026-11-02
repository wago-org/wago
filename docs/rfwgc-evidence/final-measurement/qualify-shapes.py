#!/usr/bin/env python3
import pathlib,subprocess,tarfile,io,os,json,hashlib
H=pathlib.Path(__file__).resolve().parent;R=H.parents[2];O=H/'.prepared';V=H/'shape-sizes';V.mkdir(exist_ok=True)
GO='/home/dev/.local/share/mise/installs/go/1.27.1/bin/go';rows=[];pins=json.loads((H/'plan.json').read_text())['pins']
for label,collector,mark in [('shape-before',200,192),('shape-after',192,168)]:
 tree=O/('size-'+label);tree.mkdir()
 data=subprocess.check_output(['git','archive',pins[label],'--','.',':!docs'],cwd=R)
 with tarfile.open(fileobj=io.BytesIO(data)) as tf:tf.extractall(tree,filter='data')
 (tree/'src/wago/final_shape_size_test.go').write_bytes((H/'fixtures/shape_test.go.txt').read_bytes())
 env={'PATH':str(pathlib.Path(GO).parent)+':/usr/bin:/bin','HOME':os.environ['HOME'],'GOROOT':str(pathlib.Path(GO).parents[1]),'GOTOOLCHAIN':'local','GOWORK':'off','GOMAXPROCS':'2','GOFLAGS':'-p=1','FM_EXPECT_COLLECTOR':str(collector),'FM_EXPECT_MARK':str(mark)}
 for mode in ['ordinary','wagodebug','wago_regalloccheck']:
  binary=O/f'size-{label}-{mode}.test';build=[GO,'test','-c','-o',str(binary)]+([] if mode=='ordinary' else ['-tags='+mode])+['./src/wago']
  for kind,cmd,cwd in [('build',build,tree),('check',[str(binary),'-test.run=^TestFinalPreparationShapeSize$','-test.count=1','-test.v','-test.timeout=3m'],tree/'src/wago')]:
   log=V/f'{label}-{mode}-{kind}.log'
   with log.open('w') as f:p=subprocess.run(cmd,cwd=cwd,env=env,stdout=f,stderr=subprocess.STDOUT,timeout=900)
   rows.append({'pin':pins[label],'mode':mode,'kind':kind,'argv':cmd,'cwd':str(cwd),'env':env,'exit':p.returncode,'log':str(log.relative_to(H)),'log_sha256':hashlib.sha256(log.read_bytes()).hexdigest(),'binary_sha256':hashlib.sha256(binary.read_bytes()).hexdigest() if binary.exists() else None})
   (V/'receipts.json').write_text(json.dumps(rows,indent=2)+'\n');print(label,mode,kind,p.returncode,flush=True)
   if p.returncode:break
