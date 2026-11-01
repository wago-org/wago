#!/usr/bin/env python3
"""Controlled GC placement probe; no production source edits."""
import pathlib,sys,subprocess
artifact=pathlib.Path(sys.argv[1]).resolve(); mode=sys.argv[3]
out=artifact.parent/(artifact.name+'-gc-'+mode)
out.mkdir(exist_ok=False)
s=(artifact/'main.go').read_text().replace('"time"','"time"\n "runtime"')
if mode=='during':
 s=s.replace('cancel()', 'println("boundary: during-guest-gc-enter")\n runtime.GC()\n println("boundary: during-guest-gc-return")\n cancel()')
elif mode=='before':
 s=s.replace('go func() {','println("boundary: before-guest-gc-enter")\n runtime.GC()\n println("boundary: before-guest-gc-return")\n go func() {')
else: raise SystemExit('mode must be before or during')
(out/'main.go').write_text(s)
(out/'go.mod').write_text('module tinygo-interruption-test\n\ngo 1.22\nrequire github.com/wago-org/wago v0.0.0\nreplace github.com/wago-org/wago => '+str(artifact/'native-runtime')+'\n')
(out/'go.sum').write_bytes((artifact/'go.sum').read_bytes())
subprocess.run([sys.argv[2],'build','-scheduler=threads','-gc=conservative','-tags=wago_standalone','-o',str(out/'runner'),'.'],cwd=out,check=True)
print(out/'runner')
