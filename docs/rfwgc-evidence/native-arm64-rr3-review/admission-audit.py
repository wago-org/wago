import pathlib,subprocess,os,json,re,hashlib
out=pathlib.Path(__file__).resolve().parent
h=pathlib.Path('/tmp/rr3-review-6c71/harness'); s=h.parent/'source'
w=(h/'.github/workflows/ci.yml').read_text()
block=w.split('      - name: Require reviewed exact source and harness before checkout\n',1)[1].split('      - uses:',1)[0]
script='\n'.join(x[10:] for x in block.split('        run: |\n',1)[1].splitlines())
base=dict(os.environ,QUALIFICATION_SOURCE='5847df30f1af15d10b743d90a24b1429b290d358',QUALIFICATION_DRIVER='6c71c4d6747456578f55928e900e36bb2d1449a8',WORKFLOW_SHA='6c71c4d6747456578f55928e900e36bb2d1449a8',OTHER_QUALIFICATION='false')
rows=[]
for name,changes,want in [('valid',{},0),('both-flags',{'OTHER_QUALIFICATION':'true'},1),('wrong-source',{'QUALIFICATION_SOURCE':'a'*40},1),('short-harness',{'QUALIFICATION_DRIVER':'6c71c4d'},1),('moved-workflow',{'WORKFLOW_SHA':'b'*40},1)]:
 r=subprocess.run(['bash','--noprofile','--norc','-eo','pipefail','-c',script],cwd=out,env=dict(base,**changes),capture_output=True,text=True,timeout=10)
 (out/('admission-'+name+'.log')).write_text(r.stdout+r.stderr)
 rows.append(dict(case=name,exit=r.returncode,expected_success=want==0))
 assert (r.returncode==0)==(want==0)
summary=w.split('  native-arm64-rr3-summary:\n',1)[1].split('\n  darwin-qualification:',1)[0]
needs=re.search(r'    needs: \[(.*?)\]',summary).group(1).split(', ')
cmd=next(x.strip() for x in summary.splitlines() if "python3 -c" in x)
base_results={k:{'result':'success' if k=='native-arm64-rr3' else 'skipped'} for k in needs}
cases=[('valid',base_results,True)]
for k in needs:
 for state in ['success','failure','cancelled','skipped']:
  if state==base_results[k]['result']:continue
  cases.append((k+'-'+state,dict(base_results,**{k:{'result':state}}),False))
for name,result,ok in cases:
 r=subprocess.run(['bash','-c',cmd],env=dict(os.environ,RESULTS=json.dumps(result)),capture_output=True,text=True,timeout=10)
 rows.append(dict(case='summary-'+name,exit=r.returncode,expected_success=ok))
 assert (r.returncode==0)==ok
(out/'admission-summary-audit.json').write_text(json.dumps(rows,indent=2)+'\n')
# Git blob comparison against immutable source, independent of mutable worktree files.
repo='/home/dev/.t3/worktrees/wago/experiment-rfwgc-port'
tree=subprocess.check_output(['git','ls-tree','-rz','5847df30f1af15d10b743d90a24b1429b290d358'],cwd=repo)
count=0; errors=[]
for entry in tree.split(b'\0'):
 if not entry:continue
 header,path=entry.split(b'\t',1); mode,kind,oid=header.split()
 if kind!=b'blob':continue
 f=s/os.fsdecode(path)
 data=os.readlink(f).encode() if mode==b'120000' else f.read_bytes()
 digest=hashlib.sha1(b'blob '+str(len(data)).encode()+b'\0'+data).hexdigest().encode()
 count+=1
 if digest!=oid:errors.append(os.fsdecode(path))
(out/'source-after-build-integrity.json').write_text(json.dumps(dict(blobs=count,errors=errors),indent=2)+'\n')
assert not errors
print('Admission cases:',5,'summary cases:',len(cases),'unchanged source blobs:',count)
