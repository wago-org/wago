#!/usr/bin/env python3
"""Complete diagnostics, retaining known counter FAILs; stop any unknown failure."""
import run as q
import os,signal,subprocess,json,gzip,hashlib,sys
q.env.pop('GOWORK',None)
known={'TestIntervalRegionDynamicReuse','TestIntervalRegionControlBoundaries','TestIntervalRegionControlBoundaries/if-else'}
for tags in ['wago_codegenstats','wagodebug,wago_codegenstats','wago_regalloccheck,wago_codegenstats']:
 out=q.E/('default-diagnostic-full-'+tags);out.mkdir()
 cmd=[q.G+'/bin/go','test','-json','-count=1','-timeout=30m','-tags='+tags,'./src/core/compiler/backend/railshot/...','./src/wago']
 (out/'command.json').write_text(json.dumps({'source':q.M['pin'],'cwd':str(q.S),'argv':cmd,'environment':q.env,'driver_sha256':hashlib.sha256(open(__file__,'rb').read()).hexdigest(),'stop_on_unknown_failure':True,'known_counter_failures_remain_FAIL':sorted(known)},indent=2)+'\n')
 print('RUN',out.name,flush=True);events=[];unknown=None
 with (out/'raw.log').open('w') as f:
  p=subprocess.Popen(cmd,cwd=q.S,env=q.env,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,text=True,start_new_session=True)
  for line in p.stdout:
   f.write(line);f.flush()
   try:x=json.loads(line)
   except ValueError:continue
   if x.get('Action') in ('pass','fail','skip'):events.append(x)
   if x.get('Action')=='fail' and x.get('Test') and x['Test'] not in known and unknown is None:
    unknown=x;os.killpg(p.pid,signal.SIGTERM)
  code=p.wait()
 (out/'result.json').write_text(json.dumps({'source':q.M['pin'],'exit':code,'verdict':'PASS' if code==0 else 'FAIL','events':events,'stopped_unknown_failure':unknown},indent=2)+'\n')
 with (out/'raw.log').open('rb') as f,gzip.open(out/'raw.log.gz','wb') as z:z.writelines(f)
 (out/'raw.log').unlink();print('DONE',out.name,code,flush=True)
 if unknown:sys.exit(1)
