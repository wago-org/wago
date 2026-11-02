#!/usr/bin/env python3
"""Cross-link refusal using the pristine official Darwin runtime; no execution."""
from pathlib import Path
import os,sys,tarfile
import harness as h
out=h.E/'_unpatched-control';h.safe_output(out);out.mkdir(exist_ok=False)
with tarfile.open(h.E/'tinygo0.42.0.darwin-arm64.tar.gz') as t:t.extractall(out,filter='data')
root=out/'tinygo';h.check_hashes(root,h.PIN['before'])
source=h.E/'_source';go=Path('/home/dev/.local/share/mise/installs/go/1.27.1/bin/go');compiler=Path('/home/dev/.local/share/wago-tinygo-investigation/tinygo/bin/tinygo')
env=dict(PATH=str(go.parent)+':/usr/bin:/bin',HOME=os.environ['HOME'],GOROOT=str(go.parent.parent),TINYGOROOT=str(root),GOTOOLCHAIN='local',GOENV='off',GOWORK='off',GOOS='darwin',GOARCH='arm64',GOFLAGS='-mod=readonly -p=2',GOMAXPROCS='2',XDG_CACHE_HOME=str(out/'cache'),GOCACHE=str(out/'go-cache'))
rows=[]
for mode in h.MODES:
 cmd=[str(compiler),'test','-c','-p=2','-scheduler=threads','-gc=conservative','-o',str(out/(mode+'.test'))]
 if mode!='ordinary':cmd+=['-tags='+mode]
 cmd+=['./src/core/runtime']
 rc=h.command(out,mode,cmd,source,env,1200);text=(out/(mode+'.log')).read_text()
 expected=rc!=0 and 'wagoRegisterForeignStack' in text and ('undefined' in text or 'not defined' in text or 'linker could not find symbol' in text)
 rows.append(dict(mode=mode,exit=rc,expected_missing_hook=expected,compiler_sha256=h.sha(compiler),unpatched=h.PIN['before']))
 h.write(out/'results.json',rows)
assert all(r['expected_missing_hook'] for r in rows), rows
