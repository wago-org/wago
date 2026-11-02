#!/usr/bin/env python3
import run as q
import pathlib,sys
q.env.pop('GOWORK',None)
for label,pin,s in [('port',q.M['pin'],q.S),('baseline','a3c08365f531eb3c9003e3acf3e0a822c12c31f4',pathlib.Path(q.M['scratch'])/'baseline')]:
 q.M['pin']=pin
 for mode in ['wago_codegenstats','wagodebug,wago_codegenstats','wago_regalloccheck,wago_codegenstats']:
  q.test('interval-default-'+label+'-'+mode,['./src/core/compiler/backend/railshot/amd64'],mode,['-run','^TestIntervalRegion(DynamicReuse|ControlBoundaries)$'],s)
