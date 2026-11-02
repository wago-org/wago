#!/usr/bin/env python3
import run as q
import pathlib,sys,json,hashlib,subprocess
q.env.pop('GOWORK',None)
mode=sys.argv[1]
if mode=='provenance':
 tools=[q.G+'/bin/go',q.env['WAGO_WAST2JSON'],q.env['WAGO_SPEC_INTERPRETER'],'/home/dev/.local/share/mise/installs/node/24.22.2/bin/node','/home/dev/.local/share/wago-rfwgc-opam/rfwgc/bin/ocaml','/home/dev/.local/share/wago-rfwgc-opam/rfwgc/bin/dune','/home/dev/.local/share/wago-rfwgc-opam/rfwgc/bin/menhir','/home/dev/.local/share/wago-tinygo-investigation/tinygo/bin/tinygo']
 (q.E/'tools.json').write_text(json.dumps({p:hashlib.sha256(pathlib.Path(p).read_bytes()).hexdigest() for p in tools},indent=2)+'\n')
 cat=json.loads((q.S/'corpus/catalog.json').read_text());rows=[]
 for e in cat['benchmarks']:
  p=q.S/'corpus'/e['artifact'];inputs=[]
  for n,want in e.get('command',{}).get('inputs',{}).items():
   f=q.S/'corpus'/e['command']['preopen']/n;inputs.append({'path':str(f.relative_to(q.S)),'expected':want,'actual':hashlib.sha256(f.read_bytes()).hexdigest()})
  rows.append({'id':e['id'],'artifact':e['artifact'],'expected':e['artifact_sha256'],'actual':hashlib.sha256(p.read_bytes()).hexdigest(),'inputs':inputs})
 assert len(rows)==128 and len({r['actual'] for r in rows})==121 and sum(len(r['inputs']) for r in rows)==47
 assert all(r['actual']==r['expected'] and all(i['actual']==i['expected'] for i in r['inputs']) for r in rows)
 (q.E/'corpus-identity.json').write_text(json.dumps({'source':q.M['pin'],'catalog_sha256':hashlib.sha256((q.S/'corpus/catalog.json').read_bytes()).hexdigest(),'rows':rows},indent=2)+'\n')
 for label,cmd in [('go',[tools[0],'version']),('node',[tools[3],'--version']),('wabt',[tools[1],'--version']),('ocaml',[tools[4],'-version']),('tinygo',[tools[7],'version'])]:q.run('version-'+label,cmd)
elif mode=='root':
 for m in ['ordinary','wagodebug','wago_regalloccheck']:
  if q.test('full-root-'+m,['./...'],m,['-failfast']):sys.exit(1)
  for name,pkg,tags,cwd in [('cli',['./cli/...'],'wago_runtime'+('' if m=='ordinary' else ','+m),q.S),('installer',['./...'],m,q.S/'cli/wago-installer'),('nested',['./...'],m,q.S/'bench')]:
   if q.test(name+'-'+m,pkg,tags,['-failfast'],cwd):sys.exit(1)
elif mode=='tooling':
 for m in ['ordinary','wagodebug','wago_regalloccheck']:
  tags=[] if m=='ordinary' else ['-tags='+m]
  for label,cmd in [('build',[q.G+'/bin/go','build',*tags,'./...']),('vet',[q.G+'/bin/go','vet',*tags,'./...'])]:q.run(label+'-'+m,cmd)
 q.run('generate',[q.G+'/bin/go','generate','./...'])
 q.run('docs',[q.G+'/bin/go','run','./tests/tools/docs-check'])
 for s in ['update-website-bench','update-website-startup','engine-state-oracle','fuzz-engine-state']:q.run('node-'+s,['node','--test','scripts/'+s+'.test.mjs'])
 for s in ['build-release-assets.sh','release-qualification.sh','ci-download-go-modules.sh']:q.run('script-'+s,['bash','tests/scripts/'+s])
 q.run('script-ci-riscv64',['python3','tests/scripts/ci-riscv64.py'])
 q.run('actionlint',[q.G+'/bin/go','run','github.com/rhysd/actionlint/cmd/actionlint@v1.7.7',*[str(p.relative_to(q.S)) for p in sorted((q.S/'.github/workflows').glob('*.yml'))]])
 for s in ['check-diagnostic-dce.sh','check-riscv64-diagnostics.sh','check-regalloc-code.sh']:q.run('script-'+s,['bash','scripts/'+s])
 q.run('staticcheck-bootstrap',['bash','scripts/bootstrap-staticcheck.sh','--verify'])
 q.run('staticcheck',[q.G+'/bin/go','run','./tests/tools/staticcheck-baseline','verify'])
 q.run('native-compare',['just','test','native-compare'])
 q.run('portable-386',[q.G+'/bin/go','test','-json','-count=1','./src/core/compiler/wasm','./src/core/compiler/ir','./src/core/nativeabi','./src/core/compiler/backend/railshot/shared'],extra={'GOARCH':'386','CGO_ENABLED':'0'})
