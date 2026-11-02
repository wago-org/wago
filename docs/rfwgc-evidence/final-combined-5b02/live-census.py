"""Resolve active nested TinyGo cells using exact argv/cwd, without changing drivers."""
import pathlib,json,shlex,os
E=pathlib.Path(__file__).resolve().parent;M=json.loads((E/'source.json').read_text());S=pathlib.Path(M['scratch'])/'tinygo-source'
s=json.loads((E/'final-summary.json').read_text());matches=[]
for row in s['required']:
 if row['verdict']!='INTERRUPTED' or not row['suite'].startswith('tinygo-34/'):continue
 name=row['suite'].split('/',1)[1];p=E/'tinygo-34'/(name+'.command')
 if not p.exists():continue
 expected=shlex.split(p.read_text())
 for proc in pathlib.Path('/proc').glob('[0-9]*'):
  try:
   argv=(proc/'cmdline').read_bytes().rstrip(b'\0').decode().split('\0');cwd=(proc/'cwd').resolve()
   if cwd!=S or argv!=expected:continue
   stat=(proc/'stat').read_text().rsplit(')',1)[1].split()
   if stat[0]=='Z':continue
   row['verdict']='RUNNING';matches.append({'suite':row['suite'],'pid':int(proc.name),'cwd':str(cwd),'argv':argv,'start_ticks':stat[19]})
  except (OSError,UnicodeError):continue
(E/'live-census.json').write_text(json.dumps({'source':M['pin'],'matches':matches},indent=2)+'\n')
(E/'final-summary.json').write_text(json.dumps(s,indent=2)+'\n')
print('Verified active nested cells:',len(matches))
