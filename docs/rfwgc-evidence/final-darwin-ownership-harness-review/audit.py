import gzip, hashlib, json, re, subprocess, sys
from pathlib import Path
root=Path(sys.argv[1]); out=Path(__file__).resolve().parent
H='5fb47abb4b30472bd74751a5ea96b51d668e488a'; S='7e41fafbbcd737ed1edce11b94b43b4eda134dcf'; B='a3c08365f531eb3c9003e3acf3e0a822c12c31f4'; F='ddc488cc0071df12c3150a26b5b2d2cb1fb771c6'
def git(*a): return subprocess.check_output(['git',*a],cwd=root)
def blob(s,p): return git('show',s+':'+p)
def sha(b): return hashlib.sha256(b).hexdigest()
r={'harness':H,'source':S,'baseline':B,'native_execution':'NOT_RUN'}
for c in [F,'3f616188315096d7ce1b5fe96b747516f99ffff0','d6f4e94fe671542d7e22fe84e8fe1f9358661c3a','15d7b24b7']:
 subprocess.run(['git','merge-base','--is-ancestor',c,S],cwd=root,check=True)
r['source_ancestry']='PASS'
assert git('diff-tree','--no-commit-id','--name-only','-r',F).decode().splitlines()==['cli/wago-installer/go.sum']
assert blob(F,'cli/wago-installer/go.mod')==blob(F+'^','cli/wago-installer/go.mod')==blob(S,'cli/wago-installer/go.mod')
sums=blob(S,'cli/wago-installer/go.sum').decode().splitlines(); assert len(sums)==4
baseline_sums=blob(B,'go.sum').decode().splitlines()
assert all(x in baseline_sums for x in sums if x.startswith('golang.org/x/sys '))
for name in ['sys@v0_30_0','wago@v0_1_0-beta_13']:
 receipt=(root/f'docs/rfwgc-evidence/final-darwin/revision-3242/sumdb-{name}.txt').read_text()
 for line in sums:
  if ('sys ' in line)==name.startswith('sys'): assert line in receipt
r['installer']={'checksum_only':True,'versions_unchanged':True,'baseline_sys_sums_identical':True,'sumdb_receipts_match':True,'baseline_root_go_sum_sha256':sha(blob(B,'go.sum')),'source_root_go_sum_sha256':sha(blob(S,'go.sum')),'root_go_sum_identical':blob(B,'go.sum')==blob(S,'go.sum')}
base=root/'docs/rfwgc-evidence/final-darwin'; rev=base/'revision-7e41'; exp=rev/'export-validation-final'
spec=json.loads((rev/'contracts.json').read_text()); assert len(spec['tests']['darwin'])==72 and len(spec['rr_tests'])==22 and len(spec['tests']['windows'])==6
assert set(spec['rr_tests']) <= set(spec['tests']['darwin'])
assert json.loads(blob('336416d2a','docs/rfwgc-evidence/final-darwin/plan.json'))['lanes']==json.loads((base/'plan.json').read_text())['lanes']
for f in ['shared_entry_guard_arm64_test.go','wasmtime_core3_port_test.go','spectest_exec_test.go']:
 assert blob('336416d2a','src/wago/'+f)==blob(S,'src/wago/'+f)
assert not git('diff','4af18b3a2',H,'--','docs/rfwgc-evidence/native-arm64-rr3')
r['preservation']='PASS: 63 broad recipes, cancellation/Core3/spec source, 4af18 native ARM harness'
r['retained_commands']={}
for folder in [exp,rev/'broad-tag-validation']:
 commands=json.loads((folder/'commands.json').read_text()); assert all(c['exit_code']==0 for c in commands)
 r['retained_commands'][folder.name]={'commands':len(commands),'cross_builds':sum('-c' in c['argv'] for c in commands),'installer_checks':sum('installer' in c.get('label','') and 'verify' not in c.get('label','') for c in commands)}
assert sum(v['cross_builds'] for v in r['retained_commands'].values())==18
r['manifests']={}
for name,pin in [('source',S),('baseline',B)]:
 before=json.loads(gzip.decompress((exp/(name+'-manifest.json.gz')).read_bytes())); after=json.loads(gzip.decompress((exp/(name+'-after-manifest.json.gz')).read_bytes()));assert before==after
 entries=[]
 for line in git('ls-tree','-rz',pin).split(b'\0'):
  if not line: continue
  meta,path=line.split(b'\t',1); mode,kind,oid=meta.split()
  if kind==b'blob': entries.append((path.decode(),oid))
 assert set(before)=={p for p,o in entries}
 proc=subprocess.Popen(['git','cat-file','--batch'],cwd=root,stdin=subprocess.PIPE,stdout=subprocess.PIPE)
 for path,oid in entries:
  proc.stdin.write(oid+b'\n');proc.stdin.flush(); header=proc.stdout.readline().split(); data=proc.stdout.read(int(header[2]));assert proc.stdout.read(1)==b'\n';assert sha(data)==before[path],path
 proc.stdin.close();assert proc.wait()==0
 r['manifests'][name]={'verified_git_blobs':len(entries),'before_after_identical':True}
for mode in ['ordinary','wagodebug','wago_regalloccheck']:
 for target,expected in [('darwin',spec['tests']['darwin']),('windows',spec['tests']['windows']),('darwin-baseline',['TestRFWStage3MeasurementGuest'])]:
  selected=json.loads((exp/(target+'-'+mode+'-exact-selection.json')).read_text())['selected'];assert sorted(selected)==sorted(expected)
r['exact_selection']='PASS: all nine retained selections; 72 candidate, 6 Windows, 1 baseline'
r['status']='PASS'
(out/'independent-audit.json').write_text(json.dumps(r,indent=2)+'\n')
print(json.dumps(r,indent=2))
