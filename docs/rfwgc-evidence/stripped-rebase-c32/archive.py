import pathlib,subprocess,tempfile,os,hashlib,json
repo=pathlib.Path('/home/dev/.t3/worktrees/wago/experiment-rfwgc-port');os.chdir(repo)
root=pathlib.Path('/tmp/wago-port-strip-qualification')
old=subprocess.check_output(['git','rev-parse','campaign/rfwgc-port-record'],text=True).strip()
fd,index=tempfile.mkstemp(prefix='wago-record-index-');os.close(fd);os.unlink(index)
env=dict(os.environ,GIT_INDEX_FILE=index)
subprocess.run(['git','read-tree',old],env=env,check=True)
files=[p for p in root.rglob('*') if p.is_file() and p.name!='archive-head.txt']
manifest={str(p.relative_to(root)):hashlib.sha256(p.read_bytes()).hexdigest() for p in files}
(root/'SHA256.json').write_text(json.dumps(manifest,indent=2)+'\n')
files=[p for p in root.rglob('*') if p.is_file() and p.name!='archive-head.txt']
for p in files:
 blob=subprocess.check_output(['git','hash-object','-w',str(p)],text=True).strip()
 dest='docs/rfwgc-evidence/stripped-rebase-c32/'+str(p.relative_to(root))
 subprocess.run(['git','update-index','--add','--cacheinfo','100644',blob,dest],env=env,check=True)
tree=subprocess.check_output(['git','write-tree'],env=env,text=True).strip()
new=subprocess.check_output(['git','commit-tree',tree,'-p',old],input='docs: retain stripped rebased port qualification and publication receipts\n',text=True).strip()
subprocess.run(['git','update-ref','refs/heads/campaign/rfwgc-port-record',new,old],check=True)
os.unlink(index);(root/'archive-head.txt').write_text(new+'\n');print(new)
