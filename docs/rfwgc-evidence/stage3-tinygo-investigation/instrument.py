#!/usr/bin/env python3
"""Build a boundary-traced runner against the preserved, unchanged native runtime."""
import pathlib,sys,subprocess,os
artifact=pathlib.Path(sys.argv[1]).resolve()
out=artifact.parent/(artifact.name+'-traced')
out.mkdir(exist_ok=False)
s=(artifact/'main.go').read_text()
for a,b in [
 ('rt := wago.NewRuntime()', 'println("boundary: new-runtime")\n\trt := wago.NewRuntime()'),
 ('module, err :=', 'println("boundary: adopt")\n\tmodule, err :='),
 ('instance, err :=', 'println("boundary: instantiate")\n\tinstance, err :='),
 ('ctx, cancel :=', 'println("boundary: context")\n\tctx, cancel :='),
 ('_, invokeErr :=', 'println("boundary: invoke-enter")\n\t\t_, invokeErr :='),
 ('done <- invokeErr', 'println("boundary: invoke-return", invokeErr == nil, errors.Is(invokeErr, context.Canceled))\n\t\tdone <- invokeErr'),
 ('cancel()','println("boundary: cancel-enter")\n\tcancel()\n\tprintln("boundary: cancel-return")'),
 ('if !errors.Is(err, context.Canceled)', 'println("boundary: receive", err == nil, errors.Is(err, context.Canceled))\n\t\tif !errors.Is(err, context.Canceled)')]:
 assert a in s,a
 s=s.replace(a,b)
(out/'main.go').write_text(s)
(out/'go.mod').write_text('module tinygo-interruption-test\n\ngo 1.22\nrequire github.com/wago-org/wago v0.0.0\nreplace github.com/wago-org/wago => '+str(artifact/'native-runtime')+'\n')
(out/'go.sum').write_bytes((artifact/'go.sum').read_bytes())
subprocess.run([sys.argv[2],'build','-scheduler=threads','-gc=conservative','-tags=wago_standalone','-o',str(out/'runner'),'.'],cwd=out,check=True)
print(out/'runner')
