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
SOURCE = 'f2bb27287220783e86a4ead41fee78693a5129de'
DRIVER = '6bd79ae0403ed75e7f20489d93d731ce9a42baeb'
BASE = 'docs/rfwgc-evidence/final-darwin/'
REV = BASE + 'revision-f2bb27287/'


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


def verify():
    run = read_json(HERE/'run.json')
    assert run['id'] == 41393006371 and run['head_sha'] == DRIVER
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
    seen = set()
    secret_patterns = read_json(HERE/'log-safety.json')['patterns']
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
        command_text = raw('commands.json').decode()
        assert ('-tags='+mode in command_text) == (mode != 'ordinary')
        assert js('commands.json')['environment'] == {'GOTOOLCHAIN':'local','CGO_ENABLED':'0','GOWORK':'off'}
        failures = [e for e in terminal if e['Action']=='fail']
        skips = [e for e in terminal if e['Action']=='skip']
        assert not skips
        graph = {}
        if target == 'windows':
            assert set(top['pass']) == set(spec['tests'][target]) and len(top['pass']) == 4
            assert not failures and summary['exact_census'] and summary['status']=='PASS'
            assert summary['exit_codes'] == [0,0]
        else:
            assert set(top['pass']) == set(spec['tests'][target])-{'TestRFWStage3MeasurementGuest'}
            assert top['fail'] == ['TestRFWStage3MeasurementGuest']
            assert {e['Test'] for e in failures if '/' in e.get('Test','')} == set(spec['graph_subtests'])
            assert len(failures) == 8 and len(top['pass']) == 4
            errors = [e for e in events if 'unknown arm64 optimization "gc-lifetime-analyze"' in e.get('Output','')]
            assert len(errors)==6 and {e['Test'] for e in errors}==set(spec['graph_subtests'])
            assert not summary['exact_census'] and summary['status']=='FAIL' and summary['exit_codes']==[0,1]
            for name in ['graph.wasm','scalar.wasm','noGC.wasm']:
                graph[name] = sha(raw('generated/'+name))
                assert graph[name] == spec['fixtures']['_fixtures/'+name]
        assert summary['graph_hashes']
        # Keep small, directly reviewable original receipts; large manifests/census stay zipped.
        dest=HERE/'receipts'/art['name']
        dest.mkdir(parents=True,exist_ok=True)
        for name in ['host.json','summary.json','commands.json','tests.jsonl']:
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
            assert steps['Execute exact new contracts and generated graphs']=='failure'
            assert steps['Execute correctness and retain census']=='skipped'
    logs=zipfile.ZipFile(HERE/'full-job-logs.zip')
    assert logs.testzip() is None
    aggregates = [n for n in logs.namelist() if '/' not in n and n.endswith('.txt')]
    assert len(aggregates) == 7
    report={'run':run['id'],'source':SOURCE,'harness':DRIVER,'verification':'PASS',
            'full_job_log_count':len(aggregates),'log_archive_entry_count':len(logs.namelist()),
            'jobs':collections.Counter(j['conclusion'] for j in jobs),'cells':results,
            'scope':'Offline evidence validation only. No runtime execution or timing performed.'}
    (HERE/'verification.json').write_text(json.dumps(report,indent=2)+'\n')
    print(json.dumps({'verification':'PASS','cells':len(results),'jobs':report['jobs']}))


if __name__ == '__main__':
    verify()
