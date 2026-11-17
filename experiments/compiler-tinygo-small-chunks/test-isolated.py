#!/usr/bin/env python3
"""Run real TinyGo focused tests with unchanged production source; restore tests."""
import pathlib, subprocess, tempfile, os, json
repo=pathlib.Path(__file__).resolve().parents[2];d=repo/'src/core/compiler/backend/railshot/amd64';backup=pathlib.Path(tempfile.mkdtemp(prefix='small-chunks-test-',dir='/tmp'));moved=[]
try:
 for p in d.glob('*_test.go'):
  if not p.name.startswith('stack_chunk_'):
   q=backup/p.name;p.rename(q);moved.append((q,p))
 (pathlib.Path(__file__).parent/'tinygo-exclusions.json').write_text(json.dumps([str(p.relative_to(repo)) for _,p in moved],indent=2)+'\n')
 env=dict(os.environ,GOTOOLCHAIN='local',GOMAXPROCS='2')
 result=subprocess.run(['tinygo','test','-scheduler=tasks','-gc=conservative','-no-debug','-opt=2','-stack-size=256KB','-run','Test(TinyGoStackChunkLimit|StackChunkLinksAndReset|StackChunkRecycle)','./src/core/compiler/backend/railshot/amd64'],cwd=repo,env=env)
finally:
 for q,p in moved:q.rename(p)
 backup.rmdir()
raise SystemExit(result.returncode)
