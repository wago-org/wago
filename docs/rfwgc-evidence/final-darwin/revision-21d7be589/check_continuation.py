"""Synthetic driver process failures must not suppress independent gates."""
import contextlib, io, json, pathlib, sys, tempfile
from types import SimpleNamespace
from unittest.mock import patch
HERE = pathlib.Path(__file__).resolve().parent
sys.path.insert(0,str(HERE.parent))
import run, contracts, discriminators
rows=[]
for mode in ['ordinary','wagodebug','wago_regalloccheck']:
  lanes=discriminators.lanes(mode)+[x for x in run.PLAN['lanes'] if x['mode']==mode]
  for failed_gate in ['reservation','two-result']:
    with tempfile.TemporaryDirectory() as tmp:
      root=pathlib.Path(tmp)/'source';root.mkdir()
      (root/'corpus').mkdir();(root/'corpus/catalog.json').write_text('{"benchmarks":[]}')
      fixture=root/'tests/corpus/regressions/wasmtime-core3/core/gc/const-expr-gc'
      fixture.mkdir(parents=True);(fixture/'commands.json').write_text('{"commands":[]}')
      out=pathlib.Path(tmp)/'output';executed=[]
      def capture(argv,cwd):
        if argv==['git','rev-parse','--show-toplevel']:return '/synthetic-harness'
        if argv==['git','rev-parse','HEAD']:return run.PLAN['source']
        if argv[:2]==['git','status']:return ''
        if argv[:2]==['git','submodule']:return ' abc pinned'
        if argv[:2]==['go','env']:return '{"GOOS":"darwin","GOARCH":"arm64"}'
        if argv==['go','version']:return 'go version go1.27.1 darwin/arm64'
        return 'synthetic'
      def popen(argv,**kwargs):
        lane=lanes[len(executed)];executed.append(lane['id'])
        fail=lane.get('discriminator')==failed_gate
        events=[{'Action':'pass','Test':n} for n in lane.get('expected_tests',['TestSyntheticBroad'])]
        if lane.get('discriminator')=='core3-child':
          payload=dict(protocol='2',fixture=discriminators.FIXTURE,nonce=discriminators.NONCE,modules_passed=10,modules_failed=0,modules_skipped=0,assertions_passed=18,assertions_failed=0,assertions_skipped=0)
          events.append({'Action':'output','Output':discriminators.MARKER+json.dumps(payload)})
        if fail:events=[{'Action':'fail','Package':'synthetic','Output':'synthetic process crash'}]
        return SimpleNamespace(stdout=io.StringIO(''.join(json.dumps(e)+'\n' for e in events)),wait=lambda:1 if fail else 0)
      with patch.object(sys,'argv',['run.py',str(root),str(out),'--mode',mode]), patch.object(run.platform,'system',return_value='Darwin'), patch.object(run.platform,'machine',return_value='arm64'), patch.object(run,'validate',return_value={'source':run.PLAN['source'],'driver':'0'*40}), patch.object(run,'capture',side_effect=capture), patch.object(run,'manifest',return_value={'synthetic':'hash'}), patch.object(contracts,'darwin_overlays',return_value={}), patch.object(run.subprocess,'run',return_value=SimpleNamespace(stdout='0',returncode=0)), patch.object(run.subprocess,'Popen',side_effect=popen), patch.dict(run.os.environ,{},clear=True), contextlib.redirect_stdout(io.StringIO()):
        code=run.main()
      results=json.loads((out/'results.json').read_text())
      assert code==1 and executed==[x['id'] for x in lanes]
      assert [x['id'] for x in results if x['status']=='FAIL']==[failed_gate+'-'+mode]
      assert len(results)==26 and all((out/x['id']/'raw.jsonl').exists() for x in lanes)
      rows.append(dict(mode=mode,synthetic_failure=failed_gate,exit_code=code,completed=executed,failed=[x for x in results if x['status']=='FAIL']))
(HERE/'continuation.json').write_text(json.dumps({'status':'PASS','synthetic_only':True,'cases':rows},indent=2)+'\n')
print('PASS: six synthetic failure cases; all 26 independent gates run, aggregate exit 1')
