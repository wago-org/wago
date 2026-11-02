#!/usr/bin/env python3
"""Offline verification of downloaded native evidence against immutable git objects."""
import collections
import hashlib
import json
from pathlib import Path
import subprocess
import zipfile

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]
SOURCE = '7e41fafbbcd737ed1edce11b94b43b4eda134dcf'
DRIVER = '5fb47abb4b30472bd74751a5ea96b51d668e488a'
BASE = 'docs/rfwgc-evidence/final-darwin/'
REV = BASE + 'revision-7e41/'


def sha(data):
    return hashlib.sha256(data).hexdigest()


def git(*args):
    return subprocess.check_output(['git', *args], cwd=ROOT)


def blob(rev, path):
    return git('show', rev + ':' + path)


def main_manifest(rev):
    result = {}
    proc = subprocess.Popen(['git', 'cat-file', '--batch'], cwd=ROOT,
                            stdin=subprocess.PIPE, stdout=subprocess.PIPE)
    for row in subprocess.check_output(['git', 'ls-tree', '-rz', rev], cwd=ROOT).split(b'\0'):
        if not row:
            continue
        meta, name = row.split(b'\t', 1)
        mode, kind, oid = meta.split()
        if kind != b'blob':
            continue
        proc.stdin.write(oid + b'\n')
        proc.stdin.flush()
        header = proc.stdout.readline().split()
        data = proc.stdout.read(int(header[2]))
        assert proc.stdout.read(1) == b'\n'
        result[name.decode()] = sha(data)
    proc.stdin.close()
    assert proc.wait() == 0
    return result


def read_json(path):
    return json.loads(path.read_text())


def verify_broad(z, mode, pinned, output):
    prefix = 'receipts-'+mode+'/'
    def js(n): return json.loads(z.read(prefix+n))
    plan = json.loads(blob(DRIVER, BASE+'plan.json'))
    disc = {}
    exec(compile(blob(DRIVER, BASE+'discriminators.py'), 'pinned-discriminators', 'exec'), disc)
    lanes = disc['lanes'](mode) + [x for x in plan['lanes'] if x['mode']==mode]
    assert len(plan['lanes']) == 63 and len(lanes) == 24
    assert js('execution-plan.json') == {'lanes':lanes,'unmet_gaps':plan['unmet_gaps']}
    host, summary = js('host.json'), js('summary.json')
    assert host['source']==SOURCE and host['driver']==DRIVER
    assert host['go']=='go version go1.27.1 darwin/arm64'
    assert host['driver_sha256']==sha(blob(DRIVER, BASE+'run.py'))
    assert host['plan_sha256']==sha(blob(DRIVER, BASE+'plan.json'))
    before, after = js('source-before.json'), js('source-after.json')
    assert before == after == json.loads(z.read('contracts-'+mode+'/source-manifest.json'))
    assert js('driver-manifest.json') == json.loads(z.read('contracts-'+mode+'/driver-manifest.json'))
    assert summary['source']==SOURCE and summary['source_unchanged'] and summary['driver_unchanged'] and summary['native_execution']
    assert summary['qualification']=='REQUIRES_REVIEW'
    assert summary['results']==js('results.json')
    rows=[]
    failures=[]
    for lane, result in zip(lanes, summary['results'], strict=True):
        ident=lane['id']
        command=js(ident+'/command.json')
        assert all(command[k]==v for k,v in lane.items()), ident
        assert result == js(ident+'/result.json') and result['id']==ident
        events=[]
        for line in z.read(prefix+ident+'/raw.jsonl').splitlines():
            try: events.append(json.loads(line))
            except ValueError: pass
        details=collections.defaultdict(list)
        observed_fail=[]; observed_skip=[]; passed=0
        for e in events:
            key=(e.get('Package'),e.get('Test'))
            if e.get('Action')=='output': details[key].append(e.get('Output',''))
            if e.get('Action') in ('fail','skip'):
                record={'package':key[0],'test':key[1],'detail':''.join(details[key])}
                (observed_fail if e['Action']=='fail' else observed_skip).append(record)
            if e.get('Action')=='pass' and e.get('Test'): passed+=1
        assert result['failures']==observed_fail and result['skips']==observed_skip
        assert result['failure_events']==len(observed_fail) and result['passing_test_events']==passed
        assert isinstance(result['census_exit_code'], int)
        assert (result['exit_code']==0)==(not observed_fail)
        assert z.read(prefix+ident+'/package-census.jsonl')
        if lane.get('discriminator'):
            commands = json.loads(blob(SOURCE, 'tests/corpus/regressions/wasmtime-core3/core/gc/const-expr-gc/commands.json'))
            diagnostic, exact = disc['evaluate'](lane, events, commands)
            assert diagnostic == result['diagnostic']
            assert (result['status']=='PASS') == (exact and result['exit_code']==0)
            if lane['discriminator']=='core3-child':
                assert js(ident+'/commands-source.json') == commands
                for n, h in js(ident+'/fixture-manifest.json').items():
                    assert sha(blob(SOURCE, 'tests/corpus/regressions/wasmtime-core3/core/gc/const-expr-gc/'+n)) == h
            dest = HERE/'diagnostics'/mode/ident
            dest.mkdir(parents=True,exist_ok=True)
            for n in z.namelist():
                if n.startswith(prefix+ident+'/'):
                    (dest/n.rsplit('/',1)[1]).write_bytes(z.read(n))
        rows.append({k:result[k] for k in ['id','exit_code','census_exit_code','passing_test_events','failure_events','status']} | {'skip_events':len(observed_skip)})
        failures.extend(dict(lane=ident,**e) for e in observed_fail)
    dest=HERE/'broad'/mode
    dest.mkdir(parents=True,exist_ok=True)
    for n in ['host.json','execution-plan.json','catalog-platform-census.json']:
        (dest/n).write_bytes(z.read(prefix+n))
    # Full failure details are retained in the original archives; this index locates every failure.
    (dest/'failure-index.json').write_text(json.dumps([e for e in failures],indent=2)+'\n')
    output.append({'mode':mode,'commands':len(rows),'lanes':rows})



def verify():
    spec=json.loads(blob(DRIVER,REV+'contracts.json'))
    baseline=spec['baseline']
    pinned={r:main_manifest(r) for r in (SOURCE,DRIVER,baseline)}
    for n,h in spec['source_files'].items(): assert sha(blob(SOURCE,n))==h,n
    for n,h in spec['fixtures'].items(): assert sha(blob(DRIVER,REV+n))==h,n
    assert len(spec['tests']['darwin'])==72 and len(spec['rr_tests'])==22
    assert set(spec['rr_tests']) <= set(spec['tests']['darwin'])
    run=read_json(HERE/'run.json');assert run['id']==41447853354 and run['head_sha']==DRIVER
    assert run['status']=='completed' and run['conclusion']=='failure'
    assert (HERE/'dispatch.json').read_bytes()==blob('HEAD','docs/rfwgc-evidence/native-run-41447853354/dispatch.json')
    results=[];broad=[];subs=[];hashes={}
    for art in read_json(HERE/'artifacts.json')['artifacts']:
        f=HERE/'artifacts'/(art['name']+'.zip');data=f.read_bytes()
        assert len(data)==art['size_in_bytes'] and 'sha256:'+sha(data)==art['digest']
        assert art['workflow_run']['head_sha']==DRIVER and art['workflow_run']['id']==run['id']
        z=zipfile.ZipFile(f);assert z.testzip() is None
        hashes[str(f.relative_to(HERE))]={n:{'bytes':len(b),'sha256':sha(b)} for n in z.namelist() for b in [z.read(n)]}
        target,_,mode,attempt=art['name'].split('-');assert attempt=='1'
        if target=='darwin':verify_broad(z,mode,pinned,broad)
        for prefix,isbase in ([('contracts-'+mode+'/',False),('baseline-'+mode+'/',True)] if target=='darwin' else [('',False)]):
            def js(n):return json.loads(z.read(prefix+n))
            host,summ=js('host.json'),js('summary.json');rev=baseline if isbase else SOURCE
            assert host['source']==summ['source']==rev and host['driver']==summ['driver']==DRIVER
            assert host['driver_sha256']==sha(blob(DRIVER,BASE+'contracts.py'))
            assert host['contracts_sha256']==sha(blob(DRIVER,REV+'contracts.json'))
            arch='arm64' if target=='darwin' else 'amd64'
            assert host['machine'].lower()==arch and host['go']==f'go version go1.27.1 {target}/{arch}'
            counts={}
            for kind,r in [('source',rev),('driver',DRIVER)]:
                obs=js(kind+'-manifest.json');exp=pinned[r]
                assert all(obs.get(k)==v for k,v in exp.items()),(art['name'],prefix,kind)
                extra={k:v for k,v in obs.items() if k not in exp}
                assert all(k.startswith('tests/conformance/spec-') for k in extra)
                if extra:subs.append(extra)
                counts[kind]={'git_blobs_verified':len(exp),'submodule_files':len(extra)}
            ev=[json.loads(l) for l in z.read(prefix+'tests.jsonl').splitlines() if l.strip().startswith(b'{')]
            term=[e for e in ev if e['Action'] in ('pass','fail','skip')]
            assert term==summ['terminal_events'] and not any(e['Action']!='pass' for e in term)
            top=[e['Test'] for e in term if e.get('Test') and '/' not in e['Test']]
            expected=['TestRFWStage3MeasurementGuest'] if isbase else spec['tests'][target]
            assert sorted(top)==sorted(expected)
            assert summ['exit_codes']==[0,0] and summ['status']=='PASS' and summ['unchanged'] and summ['native_execution'] and summ['exact_census'] and summ['graph_hashes']
            cmd=js('commands.json');assert cmd['expected']==expected
            flags=['-mod=readonly']+([] if mode=='ordinary' else ['-tags='+mode])
            want=[(['go','list',*flags,'-json','-test','./src/wago'],'package-census.jsonl'),(['go','test',*flags,'-json','-count=1','-timeout=15m','-run=^('+'|'.join(expected)+')$','./src/wago'],'tests.jsonl')]
            for (argv,log),(w,wl) in zip(cmd['commands'],want,strict=True):
                overlays=[a for a in argv if a.startswith('-overlay=')];assert len(overlays)==1
                assert [a for a in argv if not a.startswith('-overlay=')]==w and log==wl
            assert all(cmd['environment'][k]==v for k,v in {'GOTOOLCHAIN':'local','CGO_ENABLED':'0','GOWORK':'off','GOENV':'off'}.items())
            assert z.read(prefix+'package-census.jsonl')
            overlays=js('overlay.json')['Replace'];normalized={}
            for k,v in overlays.items():
                k=k.replace('\\','/');v=v.replace('\\','/')
                normalized[k.split('/'+('baseline' if isbase else 'source')+'/',1)[1]]=v.split('/harness/',1)[1]
            expected_overlays={'src/wago/qualification_supplement_test.go':REV+'_fixtures/'+('graph_test.go' if target=='darwin' else 'windows_admission_test.go')}
            if target=='darwin' and not isbase:expected_overlays.update({k:REV+v for k,v in spec['darwin_test_overlays'].items()})
            assert normalized==expected_overlays
            graph=[];migration=[]
            if target=='darwin':
                graph=[e['Test'] for e in term if e.get('Test','').startswith('TestRFWStage3MeasurementGuest/') and e['Test'].count('/')==2]
                assert sorted(graph)==sorted(spec['baseline_graph_subtests'] if isbase else spec['graph_subtests'])
                for n in ['graph.wasm','scalar.wasm','noGC.wasm']:assert sha(z.read(prefix+'generated/'+n))==spec['fixtures']['_fixtures/'+n]
                if not isbase:
                    migration=[old+'/'+test for old,tests in spec['migration'].items() for test in tests]
                    names=[e.get('Test') for e in term]
                    assert all(names.count(n)==1 for n in migration)
            dest=HERE/'receipts'/art['name']/('baseline' if isbase else 'candidate');dest.mkdir(parents=True,exist_ok=True)
            for n in ['host.json','summary.json','commands.json','overlay.json','tests.jsonl']:(dest/n).write_bytes(z.read(prefix+n))
            results.append({'artifact':art['name'],'baseline':isbase,'top_pass':len(top),'test_pass_events':sum(bool(e.get('Test')) for e in term),'exit_codes':summ['exit_codes'],'skips':0,'graph_leaves':graph,'migration_children':migration,'rr_top_pass':0 if isbase or target!='darwin' else len(spec['rr_tests']),'manifests':counts})
    assert len(results)==9 and len(broad)==3
    assert len(subs)==3 and all(s==subs[0] for s in subs)
    logs=zipfile.ZipFile(HERE/'full-job-logs.zip');assert logs.testzip() is None
    aggregates=[n for n in logs.namelist() if '/' not in n and n.endswith('.txt')];assert len(aggregates)==7
    hashes['full-job-logs.zip']={n:{'bytes':len(b),'sha256':sha(b)} for n in logs.namelist() for b in [logs.read(n)]}
    (HERE/'raw-member-hashes.json').write_text(json.dumps(hashes,indent=2)+'\n')
    report={'verification':'PASS','source':SOURCE,'harness':DRIVER,'baseline':baseline,'cells':results,'broad':broad,'full_job_logs':aggregates,'jobs':collections.Counter(j['conclusion'] for j in read_json(HERE/'jobs.json')['jobs']),'limits':['Submodule file hashes cross-compared across three candidate receipts; nested Git blobs not independently attested.','Runner infrastructure and compiler binaries not independently attested.','No runtime reruns or performance measurements.']}
    (HERE/'verification.json').write_text(json.dumps(report,indent=2)+'\n')
    print(json.dumps({'verification':'PASS','focused_cells':len(results),'broad_commands':sum(len(r['lanes']) for r in broad)}))

if __name__=='__main__':verify()
