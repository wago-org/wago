import json,subprocess,os,pathlib,sys
root=pathlib.Path(__file__).resolve().parents[5];rows=[]
if len(sys.argv)!=3:raise SystemExit('usage: audit.py helper_binary reads_binary')
binaries={'helper.test':str(pathlib.Path(sys.argv[1]).resolve()),'reads.test':str(pathlib.Path(sys.argv[2]).resolve())}
for case in ['struct-allocation-access','array-allocation-access','tree-survival']:
 for compiler,mode,binary in [('helper_only','handles','helper.test'),('helper_only','direct','helper.test'),('native_direct_reads','direct','reads.test')]:
  env=dict(os.environ,GC_BRIDGE_CASE=case,GC_BRIDGE_MODE=mode,GC_BRIDGE_COMPILER_MODE=compiler,GC_BRIDGE_WARMUP='3',GC_BRIDGE_SAMPLES='0',GC_BRIDGE_BATCH='5',GOMAXPROCS='1')
  r=subprocess.run([binaries[binary],'-test.run=^TestGCDirectBridgeProbe$','-test.v'],cwd=root/'src/wago',env=env,text=True,capture_output=True)
  row={'case':case,'compiler':compiler,'mode':mode,'returncode':r.returncode,'stdout':r.stdout,'stderr':r.stderr}
  for line in r.stdout.splitlines():
   if 'DIRECT_BRIDGE_RESULT ' in line:row['result']=json.loads(line.split('DIRECT_BRIDGE_RESULT ',1)[1])
  rows.append(row);pathlib.Path(__file__).with_name('audit.json').write_text(json.dumps(rows,indent=2));print(case,compiler,mode,r.returncode,flush=True)
if any(r['returncode'] or 'result'not in r for r in rows):raise SystemExit(1)
