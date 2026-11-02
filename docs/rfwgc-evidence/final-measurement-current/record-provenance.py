#!/usr/bin/env python3
"""Hash pinned tools and original evidence, without executing workloads."""
import pathlib,hashlib,json,subprocess,os,sys
H=pathlib.Path(__file__).resolve().parent;R=H.parents[2]
def sha(p):
 d=hashlib.sha256()
 with p.open('rb') as f:
  for block in iter(lambda:f.read(1048576),b''):d.update(block)
 return d.hexdigest()
def save(p,x):p.write_text(json.dumps(x,indent=2)+'\n')
if sys.argv[1]=='tools':
 expected=json.loads((H/'tools.json').read_text());files={};links={}
 for tool,rec in expected.items():
  assert sha(pathlib.Path(tool))==rec['sha256'],tool
  for p in sorted(pathlib.Path(tool).parents[1].rglob('*')):
   if p.is_symlink():links[str(p)]=os.readlink(p)
   elif p.is_file():files[str(p)]=sha(p)
 out={'files':files,'symlinks':links,'binary_versions':expected,'policy':'tool trees hashed before completed qualification and verified afterward; source trees exported from immutable pins'}
 dest=H/'tool-manifest.json'
 if dest.exists():
  assert json.loads(dest.read_text())==out,'tool drift'
  save(H/'tool-integrity.json',{'status':'PASS','files':len(files),'symlinks':len(links)})
 else:save(dest,out)
else:
 paths=subprocess.check_output(['git','ls-files','-z','--','docs/rfwgc-evidence/final-measurement'],cwd=R).decode().split('\0')
 files={p:sha(R/p) for p in paths if p}
 dest=H/'original-evidence-manifest.json'
 if dest.exists():
  assert json.loads(dest.read_text())==files,'original evidence changed'
  save(H/'original-evidence-integrity.json',{'status':'PASS','files':len(files)})
 else:save(dest,files)
 p=json.loads((H/'plan.json').read_text());rows=[]
 for rev in ['08f511fac41a77f4c0f9046f88ebc78ad05a5da6','5b02ec67afe0dce181a7f8d508e270c7ecb0ecfc']:
  paths=subprocess.check_output(['git','diff','--name-only',rev,p['pins']['port']],cwd=R).decode().splitlines()
  production=[x for x in paths if not(x.startswith(('docs/','.github/','tests/')) or x.endswith('_test.go'))]
  rows.append({'comparison':rev,'candidate':p['pins']['port'],'changed_paths':paths,'production_changes':production})
  assert not production,production
 save(H/'source-relation.json',rows)
