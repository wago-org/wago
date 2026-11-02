import run as q
import sys
for m in ['ordinary','wagodebug','wago_regalloccheck']:
 if m!='ordinary' and q.test('full-root-replay-'+m,['./...'],m,['-failfast']):sys.exit(1)
 for name,pkg,tags,cwd in [('cli',['./cli/...'],'wago_runtime'+('' if m=='ordinary' else ','+m),q.S),('installer',['./...'],m,q.S/'cli/wago-installer'),('nested',['./...'],m,q.S/'bench')]:
  if q.test(name+'-'+m,pkg,tags,['-failfast'],cwd):sys.exit(1)
