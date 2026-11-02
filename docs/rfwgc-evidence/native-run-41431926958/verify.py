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
SOURCE = 'da19f1a8d29fec7305d937a42622a198b8e79717'
DRIVER = '336416d2a006bbefded9e5b0b8539199bd6b0e3b'
BASE = 'docs/rfwgc-evidence/final-darwin/'
REV = BASE + 'revision-da19f1a8d/'


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
    run = read_json(HERE/'run.json')
    assert run['id'] == 41431926958 and run['head_sha'] == DRIVER
    assert run['status'] == 'completed' and run['conclusion'] == 'failure'
    spec = json.loads(blob(DRIVER, REV+'contracts.json'))
    assert spec['source'] == SOURCE
    for name, digest in spec['source_files'].items():
        assert sha(blob(SOURCE, name)) == digest, name
    for name, digest in spec['fixtures'].items():
        assert sha(blob(DRIVER, REV+name)) == digest, name
    pinned = {SOURCE: main_manifest(SOURCE), DRIVER: main_manifest(DRIVER)}
    submodule_receipts = []
    artifacts = read_json(HERE/'artifacts.json')['artifacts']
    assert len(artifacts) == 6
    results = []
    results_broad = []
    seen = set()
    secret_patterns = read_json(HERE.parent/'native-run-41396042416/log-safety.json')['patterns']
    import re
    for art in sorted(artifacts, key=lambda a: a['name']):
        path = HERE/'artifacts'/(art['name']+'.zip')
        assert path.stat().st_size == art['size_in_bytes']
        assert 'sha256:'+sha(path.read_bytes()) == art['digest']
        assert art['workflow_run']['id'] == run['id']
        assert art['workflow_run']['head_sha'] == DRIVER
        target, _, mode, attempt = art['name'].split('-')
        assert attempt == '1' and (target, mode) not in seen
        seen.add((target, mode))
        z = zipfile.ZipFile(path)
        assert z.testzip() is None
        for name in z.namelist():
            if name.endswith('.wasm'):
                continue
            for pattern in secret_patterns:
                assert not re.search(pattern.encode(), z.read(name)), (name, 'potential secret')
        prefix = 'contracts-'+mode+'/' if target == 'darwin' else ''
        def raw(n): return z.read(prefix+n)
        def js(n): return json.loads(raw(n))
        host, summary = js('host.json'), js('summary.json')
        for obj in (host, summary):
            assert obj['source'] == SOURCE and obj['driver'] == DRIVER
        arch = 'arm64' if target == 'darwin' else 'amd64'
        assert host['target'] == target and host['machine'].lower() == arch
        assert host['go'] == f'go version go1.27.1 {target}/{arch}'
        assert host['driver_sha256'] == sha(blob(DRIVER, BASE+'contracts.py'))
        assert host['contracts_sha256'] == sha(blob(DRIVER, REV+'contracts.json'))
        manifest_counts = {}
        for kind, rev in [('source', SOURCE), ('driver', DRIVER)]:
            observed = js(kind+'-manifest.json')
            expected = pinned[rev]
            assert all(observed.get(k) == v for k,v in expected.items()), (art['name'], kind)
            extra = set(observed)-set(expected)
            assert all(n.startswith('tests/conformance/spec-') for n in extra), sorted(extra)[:5]
            if extra:
                assert kind == 'source' and target == 'darwin'
                submodule_receipts.append({n:observed[n] for n in extra})
            manifest_counts[kind] = {'main_repository_blobs_verified':len(expected),
                                    'submodule_entries_cross_compared_only':len(extra)}
        events = [json.loads(l) for l in raw('tests.jsonl').splitlines() if l.strip()]
        terminal = [e for e in events if e['Action'] in ('pass','fail','skip')]
        assert terminal == summary['terminal_events']
        top = {a:[e['Test'] for e in terminal if e['Action']==a and e.get('Test') and '/' not in e['Test']] for a in ('pass','fail','skip')}
        counts = collections.Counter((e['Action'],e.get('Test')) for e in terminal)
        assert all(n == 1 for n in counts.values())
        assert summary['unchanged'] and summary['native_execution']
        assert js('commands.json')['expected'] == spec['tests'][target]
        overlay = js('overlay.json')['Replace']
        assert len(overlay) == 1
        key, value = next(iter(overlay.items()))
        assert key.replace('\\', '/').endswith('/source/src/wago/qualification_supplement_test.go')
        fixture = 'graph_test.go' if target == 'darwin' else 'windows_admission_test.go'
        assert value.replace('\\', '/').endswith('/harness/'+REV+'_fixtures/'+fixture)
        commands = js('commands.json')['commands']
        flags = [] if mode == 'ordinary' else ['-tags='+mode]
        expected = [(['go','list',*flags,'-json','-test','./src/wago'],'package-census.jsonl'),
                    (['go','test',*flags,'-json','-count=1','-timeout=15m','-run=^('+'|'.join(spec['tests'][target])+')$','./src/wago'],'tests.jsonl')]
        for (argv, log), (want, wantlog) in zip(commands, expected, strict=True):
            overlays = [a for a in argv if a.startswith('-overlay=')]
            assert len(overlays) == 1 and overlays[0].replace('\\','/').endswith('/overlay.json')
            assert [a for a in argv if not a.startswith('-overlay=')] == want and log == wantlog
        command_text = raw('commands.json').decode()
        assert ('-tags='+mode in command_text) == (mode != 'ordinary')
        assert js('commands.json')['environment'] == {'GOTOOLCHAIN':'local','CGO_ENABLED':'0','GOWORK':'off'}
        failures = [e for e in terminal if e['Action']=='fail']
        skips = [e for e in terminal if e['Action']=='skip']
        assert not skips
        graph = {}
        if target == 'darwin':
            verify_broad(z, mode, pinned, results_broad)
        if target == 'windows':
            assert set(top['pass']) == set(spec['tests'][target]) and len(top['pass']) == 4
            assert not failures and summary['exact_census'] and summary['status']=='PASS'
            assert summary['exit_codes'] == [0,0]
        else:
            assert set(top['pass']) == set(spec['tests'][target]) and len(top['pass']) == 5
            assert not failures and summary['exact_census'] and summary['status']=='PASS'
            assert summary['exit_codes'] == [0,0]
            assert {e['Test'] for e in terminal if '/' in e.get('Test','')} == set(spec['graph_subtests'])
            for name in ['graph.wasm','scalar.wasm','noGC.wasm']:
                graph[name] = sha(raw('generated/'+name))
                assert graph[name] == spec['fixtures']['_fixtures/'+name]
        assert summary['graph_hashes']
        # Keep small, directly reviewable original receipts; large manifests/census stay zipped.
        dest=HERE/'receipts'/art['name']
        dest.mkdir(parents=True,exist_ok=True)
        for name in ['host.json','summary.json','commands.json','overlay.json','tests.jsonl']:
            (dest/name).write_bytes(raw(name))
        results.append({'artifact':art['name'],'archive_sha256':sha(path.read_bytes()),'top_level':top,
                        'failed_leaf_count':sum('/' in e.get('Test','') for e in failures),
                        'skip_event_count':len(skips),'manifests':manifest_counts,'generated_hashes':graph,
                        'go_sha256':host['go_sha256']})
    assert len(submodule_receipts) == 3 and submodule_receipts[0] == submodule_receipts[1] == submodule_receipts[2]
    assert seen == {(t,m) for t in ('darwin','windows') for m in ('ordinary','wagodebug','wago_regalloccheck')}
    jobs = read_json(HERE/'jobs.json')['jobs']
    for job in jobs:
        if job['name'].startswith('Pinned Darwin'):
            steps = {s['name']:s['conclusion'] for s in job['steps']}
            assert steps['Execute exact new contracts and generated graphs']=='success'
            assert steps['Execute correctness and retain census']=='failure'
    logs=zipfile.ZipFile(HERE/'full-job-logs.zip')
    assert logs.testzip() is None
    raw_hashes = {}
    for archive in [HERE/'full-job-logs.zip', *sorted((HERE/'artifacts').glob('*.zip'))]:
        with zipfile.ZipFile(archive) as z:
            raw_hashes[str(archive.relative_to(HERE))] = {
                n: {'bytes': len(data), 'sha256': sha(data)}
                for n in z.namelist() for data in [z.read(n)]}
    (HERE/'raw-member-hashes.json').write_text(json.dumps(raw_hashes, indent=2)+'\n')
    aggregates = [n for n in logs.namelist() if '/' not in n and n.endswith('.txt')]
    assert len(aggregates) == 7
    report={'run':run['id'],'source':SOURCE,'harness':DRIVER,'verification':'PASS',
            'full_job_log_count':len(aggregates),'log_archive_entry_count':len(logs.namelist()),
            'broad':results_broad,
            'jobs':collections.Counter(j['conclusion'] for j in jobs),'cells':results,
            'scope':'Offline evidence validation only. No runtime execution or timing performed.'}
    (HERE/'verification.json').write_text(json.dumps(report,indent=2)+'\n')
    print(json.dumps({'verification':'PASS','cells':len(results),'jobs':report['jobs']}))


if __name__ == '__main__':
    verify()
