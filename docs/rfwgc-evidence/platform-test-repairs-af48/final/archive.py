import pathlib,subprocess,tempfile,os,json,hashlib
repo=pathlib.Path('/home/dev/.t3/worktrees/wago/experiment-rfwgc-port');os.chdir(repo)
root=pathlib.Path('/tmp/pr1048-platform-test-repair-af48');prior=pathlib.Path('/tmp/pr1048-windows-capability-repair')
old=subprocess.check_output(['git','rev-parse','campaign/rfwgc-port-record'],text=True).strip()
files={}
for prefix,path in [('final',root),('windows-slice',prior),('pre-arm64-contract',pathlib.Path('/tmp/pr1048-platform-test-repair-final'))]:
 for p in path.iterdir():
  if p.is_file() and p.suffix!='.tar' and p.name!='archive-head.txt':files[prefix+'/'+p.name]=p
manifest={name:hashlib.sha256(p.read_bytes()).hexdigest() for name,p in files.items()};(root/'SHA256.json').write_text(json.dumps(manifest,indent=2)+'\n');files['SHA256.json']=root/'SHA256.json'
fd,index=tempfile.mkstemp(prefix='wago-record-index-');os.close(fd);os.unlink(index);env=dict(os.environ,GIT_INDEX_FILE=index)
subprocess.run(['git','read-tree',old],env=env,check=True);lines=[]
for name,p in files.items():
 blob=subprocess.check_output(['git','hash-object','-w',str(p)],text=True).strip()
 lines.append('100644 '+blob+'\tdocs/rfwgc-evidence/platform-test-repairs-af48/'+name+'\n')
subprocess.run(['git','update-index','--index-info'],env=env,input=''.join(lines),text=True,check=True)
tree=subprocess.check_output(['git','write-tree'],env=env,text=True).strip();new=subprocess.check_output(['git','commit-tree',tree,'-p',old],input='docs: archive Windows capability and cross-host decoder test repairs\n',text=True).strip()
subprocess.run(['git','update-ref','refs/heads/campaign/rfwgc-port-record',new,old],check=True);os.unlink(index);(root/'archive-head.txt').write_text(new+'\n');print(new)
