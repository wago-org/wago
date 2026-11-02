#!/usr/bin/env python3
"""Bounded bootstrap and actual CLI refusal controls; no target execution."""
from pathlib import Path
import argparse,os,subprocess,sys
import harness as h
p=argparse.ArgumentParser();p.add_argument('output',type=Path);a=p.parse_args();out=a.output.resolve();h.safe_output(out);out.mkdir(exist_ok=False)
repo=h.E.parents[2];source=h.E/'_source';root=h.E/'_cache/tinygo';go='/home/dev/.local/share/mise/installs/go/1.27.1/bin/go'
head=h.capture(['git','rev-parse','HEAD'],repo)
env={'PATH':'/usr/bin:/bin','HOME':str(out)}
wrong=out/'wrong-archive';wrong.write_bytes(b'not a release')
rows=[]
for mode in h.MODES:
 rc=h.command(out,mode+'-bootstrap',[sys.executable,str(source/'tests/scripts/patch-tinygo-foreign-stack.py'),str(root)],repo,env,60)
 assert rc==0
 for kind in ['harness','archive','platform']:
  dest=out/(mode+'-'+kind)
  cmd=[sys.executable,str(h.E/'harness.py'),'native' if kind=='platform' else 'prepare','--repo',str(repo),'--archive',str(wrong if kind=='archive' else h.E/'tinygo0.42.0.darwin-arm64.tar.gz'),'--output',str(dest),'--mode',mode,'--harness','0'*40 if kind=='harness' else head,'--go',go]
  rc=h.command(out,mode+'-'+kind,cmd,repo,env,60)
  expected={'harness':'wrong harness HEAD','archive':'wrong release archive','platform':'requires native Darwin arm64'}[kind]
  assert rc!=0 and expected in (dest/'failure.json').read_text()
  # A repeat output path refuses rather than erasing failed evidence.
  old=h.files(dest);p=subprocess.run(cmd,env=env,capture_output=True,timeout=60)
  assert p.returncode!=0 and h.files(dest)==old
  rows.append(dict(mode=mode,control=kind,exit=rc,output_preserved=True))
h.write(out/'results.json',rows)
