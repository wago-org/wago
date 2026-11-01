#!/usr/bin/env python3
"""Run unchanged binary until debugger catches a signal, preserving fault evidence."""
import pathlib, subprocess, os, sys, json
base=pathlib.Path('/home/dev/.local/share/wago-tinygo-investigation/debugger')
env=dict(os.environ,LD_LIBRARY_PATH=str(base/'usr/lib/x86_64-linux-gnu'))
for i in range(int(sys.argv[2])):
 p=subprocess.run([str(base/'usr/bin/gdb'),'-nx','-batch','-x',str(pathlib.Path(__file__).with_name('catch.gdb')),sys.argv[1]],env=env,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=15)
 s=p.stdout.decode(errors='replace')
 if 'exited normally' not in s:
  print(json.dumps({'iteration':i,'debugger_exit':p.returncode,'output':s}),flush=True);sys.exit(1)
 print(json.dumps({'iteration':i,'normal_exit':True}),flush=True)
