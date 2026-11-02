#!/usr/bin/env python3
import run as q
import runpy,sys,pathlib,json
S=pathlib.Path(q.M['scratch'])/'tooling'
original=q.run
def isolated(name,cmd,cwd=q.S,extra=None):
 return original('tooling-'+name,cmd,S if cwd==q.S else cwd,extra)
q.run=isolated
sys.argv=[str(q.E/'extra.py'),'tooling']
runpy.run_path(str(q.E/'extra.py'),run_name='__main__')
