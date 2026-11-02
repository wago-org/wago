"""Full root replay after retaining PowerShell shim failure; assertions unchanged."""
import run as q
import sys,json,pathlib,hashlib
p='/home/dev/.local/share/mise/installs/powershell/7.6.8/pwsh'
q.env['PATH']=str(pathlib.Path(p).parent)+':/home/dev/.local/share/mise/installs/just/1.58.0:'+q.env['PATH']
(q.E/'powershell-tool.json').write_text(json.dumps({'path':p,'sha256':hashlib.sha256(pathlib.Path(p).read_bytes()).hexdigest(),'driver_sha256':hashlib.sha256(pathlib.Path(__file__).read_bytes()).hexdigest()},indent=2))
for m in ['ordinary','wagodebug','wago_regalloccheck']:
 if q.test('full-root-replay-'+m,['./...'],m,['-failfast']):sys.exit(1)
 for name,pkg,tags,cwd in [('cli',['./cli/...'],'wago_runtime'+('' if m=='ordinary' else ','+m),q.S),('installer',['./...'],m,q.S/'cli/wago-installer'),('nested',['./...'],m,q.S/'bench')]:
  if q.test(name+'-'+m,pkg,tags,['-failfast'],cwd):sys.exit(1)
