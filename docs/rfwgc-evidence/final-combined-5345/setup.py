import pathlib, subprocess, tarfile, tempfile, hashlib, json, shutil
E=pathlib.Path(__file__).resolve().parent; R=E.parents[2]; P='5345bd365c13963f1bf60e26bb80789ab9ead4cc'
def sha(p):return hashlib.sha256(p.read_bytes()).hexdigest()
def git(*a):return subprocess.check_output(['git',*a],cwd=R,text=True).strip()
assert git('rev-parse',P)==P
W=pathlib.Path(tempfile.mkdtemp(prefix='wago-final-5345-'))
(E/'owned-paths.json').write_text(json.dumps([str(W)],indent=2))
(E/'worktree-before.txt').write_text(git('status','--porcelain=v1')+'\n')
(E/'index-before.txt').write_text(git('diff','--cached','--raw')+'\n')
M=dict(pin=P,evidence_head=git('rev-parse','HEAD'),source=str(W/'source'),scratch=str(W))
for label,pin in [('source',P),('tooling',P),('baseline','a3c08365f531eb3c9003e3acf3e0a822c12c31f4')]:
 s=W/label;s.mkdir(); a=W/(label+'.tar')
 with a.open('wb') as f:subprocess.run(['git','archive',pin],cwd=R,stdout=f,check=True)
 with tarfile.open(a) as t:t.extractall(s,filter='data')
 (E/(label+'-files-before.json')).write_text(json.dumps({str(p.relative_to(s)):sha(p) for p in s.rglob('*') if p.is_file()},sort_keys=True))
 if label=='source':M['archive_sha256']=sha(a)
 if label=='baseline':(E/'baseline.json').write_text(json.dumps(dict(pin=pin,source=str(s),archive_sha256=sha(a),catalog_equal=(s/'corpus/catalog.json').read_bytes()==(W/'source/corpus/catalog.json').read_bytes()),indent=2))
 if label!='baseline':
  for v in ['v1','v2','v3']:
   rel='tests/conformance/spec-'+v; rev=git('ls-tree',P,rel).split()[2]
   repo=(pathlib.Path('/home/dev/Projects/wago') if v=='v3' else R)/rel
   a=W/('spec-'+v+'.tar')
   if not a.exists():
    with a.open('wb') as f:subprocess.run(['git','-C',str(repo),'archive',rev],stdout=f,check=True)
   with tarfile.open(a) as t:t.extractall(s/rel,filter='data')
   M[v]=dict(pin=rev,archive_sha256=sha(a))
  tools=s/'.tools';tools.mkdir(exist_ok=True)
  for name in ['wabt-1.0.41-linux-x64','spec-interpreter-9d36019973201a19f9c9ebb0f10828b2fe2374aa']:(tools/name).symlink_to(pathlib.Path('/home/dev/Projects/wago/.tools')/name)
  subprocess.run(['git','init','-q',str(s)],check=True)
  (s/'.git/objects/info/alternates').write_text(git('rev-parse','--path-format=absolute','--git-common-dir')+'/objects\n')
  subprocess.run(['git','-C',str(s),'read-tree',P],check=True)
  (s/'.git/HEAD').write_text(P+'\n')
(E/'source.json').write_text(json.dumps(M,indent=2)+'\n')
old=R/'docs/rfwgc-evidence/combined-store-hook-gates'; originals={}
for n in ['run.py','extra.py','lanes.py','special.py','ancillary.py','qemu-collector.py','qemu-guest.py','corpus-summary.py','tooling-isolated.py','tooling-replay.py']:
 originals[n]=sha(old/n);shutil.copyfile(old/n,E/n)
(E/'prior-driver-hashes.json').write_text(json.dumps(originals,indent=2))
print(W)
