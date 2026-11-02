"""Review-only mocked orchestration checks; never native execution."""
import contextlib, hashlib, io, json, os, subprocess, sys, tempfile
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch
OUT = Path(__file__).resolve().parents[1]
ROOT = OUT.parents[2]
HARNESS = ROOT/'docs/rfwgc-evidence/final-darwin'
sys.path.insert(0, str(HARNESS))
import run as driver
import contracts
import discriminators as d
PIN = 'da19f1a8d29fec7305d937a42622a198b8e79717'
HARNESS_PIN = '336416d2a006bbefded9e5b0b8539199bd6b0e3b'
def git(*args):
    return subprocess.check_output(['git', *args], cwd=ROOT)
report = {'scope': 'Mocked driver orchestration plus Git/static checks, no native execution', 'source': PIN, 'harness': HARNESS_PIN, 'cases': []}
assert not git('diff', HARNESS_PIN, '--', '.github/workflows/ci.yml', str(HARNESS.relative_to(ROOT)))
assert not git('diff', HARNESS_PIN+'^', HARNESS_PIN, '--', 'src')
assert driver.PLAN['source'] == contracts.SPEC['source'] == PIN
cancel = 'src/wago/shared_entry_guard_arm64_test.go'
assert git('show', PIN+':'+cancel) == git('show', '30b0813f7881a6fbb938a21535a205803bd27a0c:'+cancel)
report['cancellation_sha256'] = hashlib.sha256(git('show', PIN+':'+cancel)).hexdigest()
core = git('show', PIN+':src/wago/wasmtime_core3_port_test.go').decode()
assert 'total.modulesPassed != 215 || total.assertionsPassed != 690' in core
assert core.index('stats.modulesSkipped != 0') < core.index('total.add(stats)')
report['parent_census'] = '215/690; rejects skips before accumulation'
for leaf in contracts.SPEC['graph_subtests']:
    events = [{'Test': x, 'Action':'pass'} for x in contracts.SPEC['tests']['darwin']+contracts.SPEC['graph_subtests']]
    assert contracts.census(events,'darwin')
    assert not contracts.census([x for x in events if x['Test'] != leaf],'darwin')
    assert not contracts.census(events+[{'Test':leaf,'Action':'pass'}],'darwin')
report['graph_census'] = 'all six missing/duplicate cases rejected'
for mode in ['ordinary','wagodebug','wago_regalloccheck']:
    lanes = d.lanes(mode)
    assert len(lanes[2]['expected_tests']) == len(set(lanes[2]['expected_tests'])) == 10
    for missing in lanes[2]['expected_tests']:
        ev = [{'Test':x,'Action':'pass'} for x in lanes[2]['expected_tests'] if x != missing]
        assert not d.evaluate(lanes[2],ev,{'commands':[]})[1]
    for case in ['success','diagnosis-fails','child-exit-fails','child-malformed','child-gap','census-fails','identity-fails','source-fails','source-dirty']:
        with tempfile.TemporaryDirectory(dir=OUT/'_probes') as tmp:
            base = Path(tmp); source=base/'source'; source.mkdir(); out=base/'out'
            fixture=source/'tests/corpus/regressions/wasmtime-core3/core/gc/const-expr-gc'
            fixture.mkdir(parents=True); (fixture/'commands.json').write_text('{"commands": []}')
            (source/'corpus').mkdir(); (source/'corpus/catalog.json').write_text('{"benchmarks": []}')
            broad={'id':'broad-'+mode,'mode':mode,'cwd':'.','argv':['go','test','-json','./src/wago'],'environment':{}}
            planned=lanes+[broad]; called=[]
            def capture(argv,cwd):
                if argv == ['git','rev-parse','--show-toplevel']: return str(base/'harness')
                if argv == ['git','rev-parse','HEAD']: return '0'*40 if case=='source-fails' else PIN
                if argv[:3] == ['git','status','--porcelain']: return ' M fixture' if case=='source-dirty' else ''
                if argv[:3] == ['git','submodule','status']: return ' abc tests/conformance/spec-v1'
                if argv[:2] == ['go','env']: return '{"GOOS":"darwin","GOARCH":"arm64"}'
                if argv == ['go','version']: return 'go version go1.27.1 darwin/arm64'
                return 'mock host'
            def validate(*args):
                if case=='identity-fails': raise ValueError('identity rejected')
                return {'source':PIN,'driver':HARNESS_PIN}
            def run(argv,**kwargs):
                return SimpleNamespace(stdout='0',returncode=1 if case=='census-fails' and argv[0]=='go' else 0)
            def popen(argv,**kwargs):
                lane=planned[len(called)]; called.append(lane['id'])
                ev=[{'Action':'pass','Test':x} for x in lane.get('expected_tests',['Broad'])]
                code=0
                if lane.get('discriminator')=='core3-child':
                    payload=dict(protocol='2',fixture=d.FIXTURE,nonce=d.NONCE,modules_passed=10,modules_failed=0,modules_skipped=0,assertions_passed=18,assertions_failed=0,assertions_skipped=0)
                    if case=='child-gap': payload.update(modules_passed=9,modules_skipped=1,assertions_passed=17,assertions_skipped=1)
                    marker='broken' if case=='child-malformed' else json.dumps(payload)
                    ev.append({'Action':'output','Output':d.MARKER+marker+'\n'})
                    if case=='child-exit-fails': code=1
                if case=='diagnosis-fails' and lane.get('discriminator')=='cancellation':
                    ev.append({'Action':'fail','Test':d.CANCELLATION}); code=1
                return SimpleNamespace(stdout=io.StringIO('non-JSON diagnostic\n'+''.join(json.dumps(e)+'\n' for e in ev)),wait=lambda:code)
            with contextlib.ExitStack() as stack:
                for obj,name,val in [(driver,'capture',capture),(driver,'validate',validate),(driver,'manifest',lambda p:{'mock':'stable'}),(driver,'PLAN',dict(driver.PLAN,lanes=[broad])),(driver.platform,'system',lambda:'Darwin'),(driver.platform,'machine',lambda:'arm64'),(driver.subprocess,'run',run),(driver.subprocess,'Popen',popen)]:
                    stack.enter_context(patch.object(obj,name,val))
                stack.enter_context(patch.dict(os.environ,{},clear=True))
                stack.enter_context(patch.object(sys,'argv',['run.py',str(source),str(out),'--mode',mode]))
                stack.enter_context(contextlib.redirect_stdout(io.StringIO()))
                stack.enter_context(contextlib.redirect_stderr(io.StringIO()))
                try: code=driver.main()
                except (ValueError,SystemExit): code=2
            blocked=case in ['identity-fails','source-fails','source-dirty']
            assert (code==0)==(case=='success'),(case,code)
            assert len(called)==(0 if blocked else 4),(case,called)
            if blocked: assert not out.exists()
            report['cases'].append({'mode':mode,'case':case,'exit':code,'commands_started':called})
report['status']='PASS'
(OUT/'independent-checks.json').write_text(json.dumps(report,indent=2)+'\n')
print('PASS: all independent mocked orchestration, identity, census and source checks')
