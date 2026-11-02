#!/usr/bin/env python3
"""Retry only environment-blocked runtime lanes, using the same binaries."""
import hashlib,json,os,subprocess
from pathlib import Path
E=Path(__file__).resolve().parent
ROOT=Path('/home/dev/.cache/wago-final-codegen')
commands=json.loads((E/'commands.json').read_text())
records=[]
for c in commands['commands']:
 if c['name'] not in [m+'-'+lane for m in ['ordinary','wagodebug','wago_regalloccheck'] for lane in ['diagnostics','public']]: continue
 env={k:v for k,v in os.environ.items() if not k.startswith('WAGO_')}
 env.update(c['environment'])
 env['PATH']='/home/dev/.local/share/mise/installs/node/24.22.2/bin:/home/dev/.local/share/mise/installs/github-web-assembly-wabt/1.0.42/bin:'+env['PATH']
 log=E/'logs'/('final-'+c['name']+'.log')
 with log.open('wb') as f:
  rc=subprocess.run(c['argv'],cwd=c['cwd'],env=env,stdout=f,stderr=subprocess.STDOUT,timeout=180).returncode
 records.append(c|{'name':'final-'+c['name'],'exit':rc,'log':str(log.relative_to(E)),'environment':c['environment']|{'PATH':env['PATH']},'binary_sha256':hashlib.sha256(Path(c['argv'][1]).read_bytes()).hexdigest()})
 print(records[-1]['name'],rc,flush=True)
(E/'retry-commands.json').write_text(json.dumps({'source':commands['source'],'commands':records},indent=2)+'\n')
