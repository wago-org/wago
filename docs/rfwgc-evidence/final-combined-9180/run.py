#!/usr/bin/env python3
"""Correctness-only immutable-source gates, with durable commands and raw output."""
import os,sys,json,pathlib,subprocess,hashlib,gzip,signal,datetime
E=pathlib.Path(__file__).resolve().parent
M=json.loads((E/'source.json').read_text()); S=pathlib.Path(M['source']); G='/home/dev/.local/share/mise/installs/go/1.27.1'
env={k:v for k,v in os.environ.items() if k in ['HOME','USER','LANG','TMPDIR','PATH','TERM']}
for k in list(env):
 if k.startswith('WAGO_'):env.pop(k)
env.update(GOROOT=G,GOTOOLCHAIN='local',GOMAXPROCS='2',GOFLAGS='-p=2',DOTNET_SYSTEM_GLOBALIZATION_INVARIANT='1',WAGO_SPEC_INTERPRETER=str(S/'.tools/spec-interpreter-9d36019973201a19f9c9ebb0f10828b2fe2374aa/wasm'),WAGO_SPEC_INTERPRETER_REVISION='9d36019973201a19f9c9ebb0f10828b2fe2374aa',WAGO_WAST2JSON=str(S/'.tools/wabt-1.0.41-linux-x64/bin/wast2json'),WAGO_WABT_VERSION='1.0.41',WAGO_APP_CORPUS_REFERENCE='all',WAGO_CORPUS_TIMEOUT='1m')
env['PATH']=':'.join(['/home/dev/.local/share/mise/installs/powershell/7.6.8','/home/dev/.local/share/mise/installs/just/1.58.0',G+'/bin','/home/dev/.local/share/mise/installs/node/24.22.2/bin','/home/dev/.local/share/wago-tinygo-investigation/tinygo/bin',str(S/'.tools/wabt-1.0.41-linux-x64/bin'),'/home/dev/.local/share/wago-rfwgc-opam/rfwgc/bin',env['PATH']])
D=hashlib.sha256(pathlib.Path(__file__).read_bytes()).hexdigest()
def run(name,cmd,cwd=S,extra=None):
 assert not (E/'SAFETY-STOP.json').exists(), 'unsafe invariant: execution stopped'
 cmd=[{'python3':'/usr/bin/python3','bash':'/usr/bin/bash','tinygo':'/home/dev/.local/share/wago-tinygo-investigation/tinygo/bin/tinygo','node':'/home/dev/.local/share/mise/installs/node/24.22.2/bin/node','just':'/home/dev/.local/share/mise/installs/just/1.58.0/just'}.get(cmd[0],cmd[0]),*cmd[1:]]
 out=E/name;out.mkdir(exist_ok=False); ev=env| (extra or {})
 (out/'command.json').write_text(json.dumps({'source':M['pin'],'cwd':str(cwd),'argv':cmd,'environment':ev,'driver_sha256':D},indent=2)+'\n')
 print('RUN',name,flush=True)
 with (out/'raw.log').open('w') as f:
  p=subprocess.Popen(cmd,cwd=cwd,env=ev,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,text=True,start_new_session=True)
  (out/'process.json').write_text(json.dumps({'pid':p.pid,'started_utc':datetime.datetime.now(datetime.timezone.utc).isoformat()}))
  for line in p.stdout:
   f.write(line);f.flush()
   # Compiler rejection and detected data races prohibit any later qualification execution.
   if any(x in line for x in ['WARNING: DATA RACE','regalloccheck: function','fatal error:','internal compiler error in function','SIGSEGV: segmentation violation']):
    (E/'SAFETY-STOP.json').write_text(json.dumps({'source':M['pin'],'command':cmd,'gate':name,'line':line,'observed_utc':datetime.datetime.now(datetime.timezone.utc).isoformat()},indent=2))
    os.killpg(p.pid,signal.SIGTERM)
  p.wait()
 events=[]
 for line in (out/'raw.log').read_text(errors='replace').splitlines():
  try:x=json.loads(line)
  except ValueError:continue
  if x.get('Action') in ['pass','fail','skip']:events.append(x)
 result={'source':M['pin'],'exit':p.returncode,'verdict':'PASS' if p.returncode==0 else 'FAIL','events':events,'raw_sha256':hashlib.sha256((out/'raw.log').read_bytes()).hexdigest(),'driver_unchanged':D==hashlib.sha256(pathlib.Path(__file__).read_bytes()).hexdigest()}
 (out/'result.json').write_text(json.dumps(result,indent=2)+'\n')
 with (out/'raw.log').open('rb') as f,gzip.open(out/'raw.log.gz','wb') as z:z.writelines(f)
 (out/'raw.log').unlink()
 print('DONE',name,p.returncode,flush=True)
 return p.returncode

def test(name,pkgs,mode='ordinary',extraargs=(),cwd=S,extra=None):
 tags=[] if mode=='ordinary' else ['-tags='+mode]
 return run(name,[G+'/bin/go','test','-json','-count=1','-timeout=60m',*tags,*extraargs,*pkgs],cwd,extra)
if __name__=='__main__':
 group=sys.argv[1]
 if group=='root':
  for mode in ['ordinary','wagodebug','wago_regalloccheck']:
   # Failfast prevents later tests in a failing package; incomplete suites stay FAIL.
   if test('root-'+mode,['./...'],mode,['-failfast']):sys.exit(1)
   test('cli-'+mode,['./cli/...'],('wago_runtime' if mode=='ordinary' else 'wago_runtime,'+mode),['-failfast'])
   test('installer-'+mode,['./...'],mode,['-failfast'],S/'cli/wago-installer')
   test('nested-'+mode,['./...'],mode,['-failfast'],S/'bench')
 elif group=='supplement':
  for mode in ['ordinary','wagodebug','wago_regalloccheck']:
   for label,pkgs,args in [('collector',['./src/core/runtime/gc/...'],[]),('race',['./src/core/runtime/gc/...','./src/core/runtime','./src/wago','./tests/integration/...'],['-race']),('guard',['./src/core/runtime','./src/wago'],[])]:
    if test(label+'-'+mode,pkgs,mode+(',wago_guardpage' if label=='guard' else '') if mode!='ordinary' else ('wago_guardpage' if label=='guard' else mode),['-failfast',*args]):sys.exit(1)
 elif group=='spec':
  for mode in ['ordinary','wagodebug','wago_regalloccheck']:
   for v in [1,2,3]:
    if test('spec-v'+str(v)+'-'+mode,['./src/wago'],mode,['-failfast','-run','^TestSpecSuiteExec$'],extra={'WAGO_SPECTEST_DIR':str(S/('tests/conformance/spec-v'+str(v))),'WAGO_SPEC_VERSION':str(v)+'.0','WAGO_SPEC_REQUIRE_ZERO_GAPS':'1'}):sys.exit(1)
 elif group=='corpus':
  for mode in ['ordinary','wagodebug','wago_regalloccheck']:
   if test('corpus-'+mode,['./suite','-args','-wago.corpus=all'],mode,['-failfast','-run','^(TestCorpus|TestCorpusSemanticExec|TestCatalog.*|TestValidateCorpusModule.*|TestApplicationCorpusRuns)$'],S/'bench'):sys.exit(1)
