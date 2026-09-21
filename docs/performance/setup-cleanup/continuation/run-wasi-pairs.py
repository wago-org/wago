#!/usr/bin/env python3
"""Fixed paired processes. Each invocation owns a new output directory."""
import argparse
import contextlib
import json
import re
from pathlib import Path
import sys
sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from measurement import TemperatureGuard, cpu_temperature_sensor, ROOT, digest, executable, fresh_directory, identity, rows, run_process, settings

CASES = {
    'worker-lifetime': (['-test.bench', '^BenchmarkWorkerLifecycleDiagnostic$/^(tiny|many_funcs|json-as|lua)$/^callers=(1|4|16)$/^(backend|full)$/^(raw|policy|public)$/^requested=(0|1|2|4)$', '-test.benchtime', '100ms', '-wago.corpus', 'tiny,many_funcs,json-as,lua', '-wago.bench.lifecycle'], ['BenchmarkWorkerLifecycleDiagnostic/tiny/callers=1/backend/raw/requested=1', 'BenchmarkWorkerLifecycleDiagnostic/json-as/callers=16/full/public/requested=0']),
    'full': (['-test.bench', '.', '-test.benchtime', '1s', '-test.timeout', '0', '-wago.corpus', 'all'], ['BenchmarkCompile_wago','BenchmarkCompile_wazero']),
    'focused': (['-test.bench', '^Benchmark(CommandExec|Instantiate|Exec|ExecCallOverhead_wago)$/^(cjson|tinyxml2|utf8proc|pcre2|tiny)(\\.|$)', '-test.benchtime', '200ms', '-wago.corpus', 'cjson,tinyxml2,utf8proc,pcre2,tiny'], ['BenchmarkCommandExec/cjson', 'BenchmarkCommandExec/tinyxml2']),
    'phases': (['-test.bench', '^BenchmarkCommandLifecycleDiagnostic$', '-test.benchtime', '1000x', '-wago.corpus', 'cjson,tinyxml2', '-wago.bench.lifecycle'], ['BenchmarkCommandLifecycleDiagnostic/minimal-wasi/Imports', 'BenchmarkCommandLifecycleDiagnostic/cjson/Imports', 'BenchmarkCommandLifecycleDiagnostic/tinyxml2/Imports']),
    'host-owned': (['-test.bench', '^BenchmarkWASI(HostCall|FileCall|OwnedLifecycle)Diagnostic$', '-test.benchtime', '200ms', '-wago.corpus', 'cjson,tinyxml2', '-wago.bench.lifecycle'], ['BenchmarkWASIHostCallDiagnostic/calls=1/lifecycle=false', 'BenchmarkWASIHostCallDiagnostic/calls=1024/lifecycle=false', 'BenchmarkWASIOwnedLifecycleDiagnostic/ProviderSetupClose', 'BenchmarkWASIFileCallDiagnostic/calls=1', 'BenchmarkWASIFileCallDiagnostic/calls=1024']),
    'smoke': (['-test.bench', '^BenchmarkCommandLifecycleDiagnostic$/^minimal-wasi$/^Imports$', '-test.benchtime', '2x', '-wago.corpus', 'cjson', '-wago.bench.lifecycle'], ['BenchmarkCommandLifecycleDiagnostic/minimal-wasi/Imports']),
}


def selected_cases(path):
    selection = json.loads(path.read_text())
    names = selection.get('benchmarks')
    if not isinstance(names, list) or not names or any(not isinstance(n, str) or not n.startswith('Benchmark') or any(not part or '\n' in part for part in n.split('/')) for n in names):
        raise ValueError('selection must contain valid benchmark names')
    if len(names) != len(set(names)):
        raise ValueError('duplicate selected benchmark')
    corpus = selection.get('corpus', {})
    if not isinstance(corpus, dict) or not all(k in names and isinstance(v, str) and v for k, v in corpus.items()):
        raise ValueError('invalid selected corpus')
    cases = {}
    for index, name in enumerate(names, 1):
        pattern = '/'.join('^'+re.escape(part)+'$' for part in name.split('/'))
        cases[f'selected-{index:03d}'] = (['-test.bench', pattern, '-test.benchtime', '200ms', '-wago.corpus', corpus.get(name, 'all'), '-wago.bench.lifecycle'], [name])
    return cases, selection


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--output', required=True, type=Path)
    p.add_argument('--binaries', type=Path, help='directory from reproduce-provider.sh')
    p.add_argument('--baseline', type=Path)
    p.add_argument('--candidate', type=Path)
    p.add_argument('--build-metadata', type=Path)
    p.add_argument('--selection', type=Path, help='JSON with an exact benchmarks list; replaces --cases')
    p.add_argument('--samples', type=int, default=20)
    p.add_argument('--timeout', type=int, default=300)
    p.add_argument('--cases', nargs='+', choices=CASES, default=['focused', 'phases', 'host-owned'])
    p.add_argument('--max-temp', type=float, help='stop a process at this CPU temperature in Celsius')
    p.add_argument('--start-temp', type=float, default=65)
    p.add_argument('--cool-seconds', type=float, default=1)
    p.add_argument('--cool-timeout', type=float, default=600)
    p.add_argument('--gomaxprocs', type=int, default=16)
    p.add_argument('--cpus', default='0-15')
    a = p.parse_args()
    if a.samples < 1 or len(a.cases) != len(set(a.cases)):
        p.error('positive sample count and unique cases required')
    if a.binaries:
        if a.baseline or a.candidate:
            p.error('use --binaries or explicit binary paths, not both')
        a.baseline, a.candidate = [a.binaries/f'wasi-{label}.test' for label in ['baseline', 'candidate']]
        a.build_metadata = a.build_metadata or a.binaries/'build.json'
    if not a.baseline or not a.candidate or not a.build_metadata:
        p.error('binary paths and build metadata are required')
    binaries = {k: executable(v) for k, v in [('baseline', a.baseline), ('candidate', a.candidate)]}
    build = json.loads(a.build_metadata.read_text())
    for label, binary in binaries.items():
        if build['binaries'][label]['sha256'] != digest(binary):
            raise ValueError(f'{label} binary does not match build metadata')
    cases = CASES
    selection = None
    if a.selection:
        cases, selection = selected_cases(a.selection)
        a.cases = list(cases)
    env, prefix = settings(a.gomaxprocs, a.cpus)
    sensor = cpu_temperature_sensor() if a.max_temp is not None else None
    if sensor and not 0 < a.start_temp < a.max_temp:
        p.error('start temperature must be below stop temperature')
    out = fresh_directory(a.output)
    meta = identity(env, binaries)
    meta.update(build=build, timeout_seconds=a.timeout, samples=a.samples, cases=a.cases, cpus=a.cpus, order='AB, BA alternating', status='running', records=[])
    if selection is not None:
        meta['selection'] = selection
        meta['selection_sha256'] = digest(a.selection)
    if sensor:
        meta['thermal'] = dict(sensor=str(sensor), maximum_celsius=a.max_temp, start_celsius=a.start_temp, stable_seconds=a.cool_seconds, wait_limit_seconds=a.cool_timeout)
    metadata = out/'run.json'
    metadata.write_text(json.dumps(meta, indent=2)+'\n')
    with contextlib.ExitStack() as stack:
        thermal = None
        if sensor:
            log = stack.enter_context((out/'temperature.jsonl').open('x'))
            thermal = TemperatureGuard(sensor, a.max_temp, a.start_temp, a.cool_seconds, a.cool_timeout, log)
        leaves = {}
        for sample in range(1, a.samples+1):
            for label in (['baseline', 'candidate'] if sample % 2 else ['candidate', 'baseline']):
                for case in a.cases:
                    args, required = cases[case]
                    command = prefix+[str(binaries[label]), '-test.run', '^$', '-test.count', '1', '-test.benchmem', *args]
                    path = out/f'{sample:03d}-{label}-{case}.txt'
                    try:
                        record = run_process(command, path, env, timeout=a.timeout, thermal=thermal)
                    except Exception as error:
                        meta['status'] = 'failed'
                        meta['failure'] = dict(sample=sample, label=label, case=case, error=str(error))
                        metadata.write_text(json.dumps(meta, indent=2)+'\n')
                        raise
                    parsed = rows(path.read_text())
                    if not set(required) <= parsed.keys():
                        raise ValueError(f'missing required leaves for {case}: {required}')
                    names = sorted(parsed)
                    if selection is not None and names != required:
                        raise ValueError(f'unexpected leaves for {case}: {names}')
                    if case == 'full' and len(names) != 984:
                        raise ValueError(f'full screen expected 984 leaves, found {len(names)}')
                    if case in leaves and leaves[case] != names:
                        raise ValueError(f'mixed benchmark leaves for {case}')
                    leaves[case] = names
                    record.update(sample=sample, label=label, case=case, run_id=meta['run_id'], environment=meta['environment'], leaves=names)
                    meta['records'].append(record)
                    metadata.write_text(json.dumps(meta, indent=2)+'\n')
                    print(f'{sample}/{a.samples} {label} {case}', flush=True)
    meta['status'] = 'complete'
    metadata.write_text(json.dumps(meta, indent=2)+'\n')


if __name__ == '__main__':
    main()
