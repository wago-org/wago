"""Read-only validation and explicit limited-provenance reuse; no tests rerun."""
import pathlib,json,hashlib,gzip,subprocess,re
E=pathlib.Path(__file__).resolve().parent;R=E.parents[2];F=R/'docs/rfwgc-evidence/darwin-tinygo-fixture-repair';COMMIT='375e47bb81ee2969615cf48ec31b94eb1edf8daf';PIN='4913c91e1ad5636543d0ea7eed740b16de1b9bf2'
def sha(p):return hashlib.sha256(p.read_bytes()).hexdigest()
def git(*args):return subprocess.check_output(['/usr/bin/git',*args],cwd=R)
sealed={}
for line in (F/'SHA256SUMS').read_text().splitlines():
 h,name=line.split(None,1);name=name.lstrip('*');p=F/name
 assert sha(p)==h,name
 assert p.read_bytes()==git('show',COMMIT+':'+str(p.relative_to(R))),name
 sealed[name]=h
for name in ['SHA256SUMS','linux-tool.json','linux-integrity.json','linux-resume-results.json','linux-source-before.json.gz','linux-resume-location.txt']:
 p=F/name;assert p.read_bytes()==git('show',COMMIT+':'+str(p.relative_to(R)));sealed[name]=sha(p)
T=json.loads((F/'linux-tool.json').read_text());I=json.loads((F/'linux-integrity.json').read_text());assert T['source']==I['source']==PIN
W=pathlib.Path((F/'linux-resume-location.txt').read_text().strip());S=W/'source';manifest=json.loads(gzip.decompress((F/'linux-source-before.json.gz').read_bytes()))
assert sha(W/'source.tar')==T['archive_sha256']
blobs={}
for row in git('ls-tree','-rz',PIN).split(b'\0'):
 if row:
  m,n=row.split(b'\t',1);mode,kind,oid=m.decode().split()
  if kind=='blob':blobs[n.decode()]=oid
for n,h in manifest.items():
 data=(S/n).read_bytes();assert hashlib.sha256(data).hexdigest()==h,n
 assert hashlib.sha1(b'blob '+str(len(data)).encode()+b'\0'+data).hexdigest()==blobs[n],n
assert len(manifest)==I['verified_git_blobs']==6151
assert sha(pathlib.Path(T['compiler']))==T['compiler_sha256']
for n,h in I['tool_hashes'].items():assert sha(pathlib.Path(T['environment']['TINYGOROOT'])/n)==h,n
rows=json.loads((F/'linux-resume-results.json').read_text());verified=[]
for row in rows:
 name=row['kind']+'-'+row['mode'];assert row['build_command'][0]==T['compiler'] and row['build_exit']==0
 assert row['build_command'][-1]=='./src/core/runtime' and '-scheduler=threads' in row['build_command'] and '-gc=conservative' in row['build_command']
 tags=[] if row['mode']=='ordinary' else ['-tags='+row['mode']];assert all(x in row['build_command'] for x in tags)
 binary=pathlib.Path(row['run_command'][0]);assert sha(binary)==row['binary_sha256'];assert int.from_bytes(binary.read_bytes()[18:20],'little')==62
 assert row['run_command'][1:]==['-test.v','-test.run=^TestTinyGoNativeResumeUsesPhysicalStackTop$']
 log=(F/(name+'-run.log')).read_text();assert '--- SKIP:' not in log
 if row['kind']=='positive':assert row['run_exit']==0 and log.count('--- PASS: TestTinyGoNativeResumeUsesPhysicalStackTop')==1 and '--- FAIL:' not in log
 else:assert row['kind']=='wrong-field' and row['run_exit']!=0 and '--- FAIL: TestTinyGoNativeResumeUsesPhysicalStackTop' in log
 verified.append(dict(row,raw_sha256=sha(F/(name+'-run.log')),build_log_sha256=sha(F/(name+'-build.log'))))
assert len(rows)==6 and {r['mode'] for r in rows if r['kind']=='positive'}=={'ordinary','wagodebug','wago_regalloccheck'}
# Verify no current executable/cwd belongs to this other worker's recorded export.
active=[]
for p in pathlib.Path('/proc').glob('[0-9]*'):
 try:
  args=(p/'cmdline').read_bytes().decode(errors='replace');cwd=str((p/'cwd').resolve())
  if str(W) in args or cwd.startswith(str(W)):active.append(int(p.name))
 except OSError:pass
assert not active,active
provenance=F/'launcher-provenance.json';extra=json.loads(provenance.read_text()) if provenance.exists() else None
(E/'reused-4913-tinygo.json').write_text(json.dumps({'source':PIN,'evidence_commit':COMMIT,'verified_export_blobs':len(manifest),'archive_sha256':T['archive_sha256'],'restored_source_matches_git':True,'tools':T,'patch_hashes':I['tool_hashes'],'serialized_command_recipe_sha256':sha(F/'linux-resume-results.json'),'contemporaneous_launcher_file_hash':None,'launcher_limit':'Executed via shell here-document; no retained launcher file/hash. Parent explicitly authorizes recipe/tool/env/source/raw proof reuse with this limit, not reconstructed launcher provenance.','parent_launcher_provenance':extra,'parent_launcher_provenance_sha256':sha(provenance) if provenance.exists() else None,'sealed_files':sealed,'receipts':verified,'positive_passes':3,'expected_negative_failures':3,'owned_commands_finished_verified':True,'no_test_rerun':True,'not_final_pin_execution':True},indent=2)+'\n')
print('Verified reuse: 4913 actual TinyGo positive3 PASS, negative3 expectedFAIL; launcher-file hash absent as disclosed')
