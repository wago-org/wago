#!/usr/bin/env python3
"""Correctness-only process replay. No clocks, throughput, or benchmark statistics."""
import subprocess,sys,json,pathlib,hashlib
exe=pathlib.Path(sys.argv[1]).resolve()
print(json.dumps({'binary':str(exe),'sha256':hashlib.sha256(exe.read_bytes()).hexdigest()}),flush=True)
for i in range(int(sys.argv[2])):
 try:
  r=subprocess.run([str(exe)],stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=5)
  print(json.dumps({'iteration':i,'exit':r.returncode,'output':r.stdout.decode(errors='replace')}),flush=True)
  if r.returncode: sys.exit(1)
 except subprocess.TimeoutExpired:
  print(json.dumps({'iteration':i,'timeout':True}),flush=True); sys.exit(2)
