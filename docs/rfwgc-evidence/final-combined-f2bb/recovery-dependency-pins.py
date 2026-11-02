import pathlib,json,hashlib,subprocess
E=pathlib.Path(__file__).resolve().parent;R=E.parents[2];m=json.loads((E/'source.json').read_text());rows=[]
for v in ['v1','v2','v3']:
 rel='tests/conformance/spec-'+v
 pin=subprocess.check_output(['git','rev-parse',m['pin']+':'+rel],cwd=R,text=True).strip();assert pin==m[v]['pin']
 repo=(pathlib.Path('/home/dev/Projects/wago') if v=='v3' else R)/rel
 rows.append((v,str(repo),pin,m[v]['archive_sha256']))
b=json.loads((E/'baseline.json').read_text());rows.append(('baseline',str(R),b['pin'],b['archive_sha256']));out=[]
for label,repo,pin,want in rows:
 p=subprocess.Popen(['git','-C',repo,'archive',pin],stdout=subprocess.PIPE);h=hashlib.sha256()
 for chunk in iter(lambda:p.stdout.read(1024*1024),b''):h.update(chunk)
 assert p.wait()==0 and h.hexdigest()==want,(label,h.hexdigest(),want)
 out.append(dict(label=label,repository=repo,pin=pin,fresh_archive_sha256=h.hexdigest(),recorded_archive_sha256=want,match=True))
(E/'recovery-dependency-pins.json').write_text(json.dumps(out,indent=2));print('verified',len(out),'dependency archive pins')
