import pathlib,json,re,os,subprocess,hashlib
out=pathlib.Path(__file__).resolve().parent
h=pathlib.Path('/tmp/rr3-review-6c71/harness'); s=h.parent/'source'
p=json.loads((h/'docs/rfwgc-evidence/native-arm64-rr3/plan.json').read_text())
old=set(re.findall(r'^--- PASS: (\w+)',(h/'docs/rr3-retirement-repair/integrated-5847df30f/ordinary-focused.log').read_text(),re.M)); new=set(p['suites']['focused']['tests'])
go='/home/dev/.local/share/mise/installs/go/1.27.1/bin/go'
env=dict(os.environ,GOROOT=str(pathlib.Path(go).parent.parent),GOTOOLCHAIN='local',GOENV='off',GOWORK='off',GOOS='linux',GOARCH='arm64',CGO_ENABLED='0')
rows=[]
for mode in ['ordinary','wagodebug','wago_regalloccheck']:
 cmd=[go,'list','-json',* ([] if mode=='ordinary' else ['-tags='+mode]),'./src/wago']
 r=subprocess.run(cmd,cwd=s,env=env,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True,timeout=60)
 (out/(mode+'-go-list.json')).write_text(r.stdout)
 assert r.returncode==0,r.stderr
 meta=json.loads(r.stdout)
 names=set()
 for f in meta['TestGoFiles']:
  names.update(re.findall(r'^func (Test\w+)\(', (s/'src/wago'/f).read_text(),re.M))
 regex=re.compile(r'^Test(RR|Design|R9(H2|Ownership)|GCExternConversion|HostCallNeedsGCSuspension|GCAdmission|GC.*Invocation|GC.*Start)')
 expected={n for n in names if regex.match(n)}
 rows.append(dict(mode=mode,expected_count=len(expected),missing=sorted(expected-new),extra=sorted(new-expected),old_unavailable=sorted((old-new)&names)))
 assert expected==new and not ((old-new)&names)
exclusions={}
for name in sorted(old-new):
 matches=[]
 for f in (s/'src/wago').glob('*test.go'):
  t=f.read_text()
  if re.search(r'^func '+name+r'\(',t,re.M):matches.append(dict(file=f.name,build=t.splitlines()[0]))
 exclusions[name]=matches
assert len(old)==100 and len(new)==85
assert (h/'docs/rr3-retirement-repair/timer-capability.go.txt').read_bytes()==(h/'docs/rfwgc-evidence/native-arm64-rr3/timer-capability.go.txt').read_bytes()
(out/'census-audit.json').write_text(json.dumps(dict(prior_amd64_count=len(old),native_focused_count=len(new),excluded=exclusions,added=sorted(new-old),modes=rows,timer_byte_identical=True),indent=2)+'\n')
print(json.dumps(rows,indent=2))
