#!/usr/bin/env python3
import run as q
import sys,pathlib
q.env.pop('GOWORK',None)
mode=sys.argv[1];suffix=sys.argv[2] if len(sys.argv)>2 else 'first'
args=[] if suffix=='full' else ['-failfast']
raise SystemExit(q.test('diagnostic-'+mode+'-'+suffix,['./src/core/compiler/backend/railshot/...','./src/wago'],mode,args,extra={'WAGO_SHARED_SCALAR':'0'}))
