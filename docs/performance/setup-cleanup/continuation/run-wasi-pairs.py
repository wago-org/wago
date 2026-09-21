#!/usr/bin/env python3
"""Fixed paired processes. Each invocation owns a new output directory."""
import argparse
import json
from pathlib import Path
import sys
sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from measurement import ROOT, digest, executable, fresh_directory, identity, rows, run_process, settings

CASES = {
    'focused': (['-test.bench', '^Benchmark(CommandExec|Instantiate|Exec|ExecCallOverhead_wago)$/^(cjson|tinyxml2|utf8proc|pcre2|tiny)(\\.|$)', '-test.benchtime', '200ms', '-wago.corpus', 'cjson,tinyxml2,utf8proc,pcre2,tiny'], ['BenchmarkCommandExec/cjson', 'BenchmarkCommandExec/tinyxml2']),
    'phases': (['-test.bench', '^BenchmarkCommandLifecycleDiagnostic$', '-test.benchtime', '1000x', '-wago.corpus', 'cjson,tinyxml2', '-wago.bench.lifecycle'], ['BenchmarkCommandLifecycleDiagnostic/minimal-wasi/Imports', 'BenchmarkCommandLifecycleDiagnostic/cjson/Imports', 'BenchmarkCommandLifecycleDiagnostic/tinyxml2/Imports']),
    'host-owned': (['-test.bench', '^BenchmarkWASI(HostCall|OwnedLifecycle)Diagnostic$', '-test.benchtime', '200ms', '-wago.corpus', 'cjson,tinyxml2', '-wago.bench.lifecycle'], ['BenchmarkWASIHostCallDiagnostic/calls=1/lifecycle=false', 'BenchmarkWASIHostCallDiagnostic/calls=1024/lifecycle=false', 'BenchmarkWASIOwnedLifecycleDiagnostic/ProviderSetupClose']),
    'smoke': (['-test.bench', '^BenchmarkCommandLifecycleDiagnostic$/^minimal-wasi$/^Imports$', '-test.benchtime', '2x', '-wago.corpus', 'cjson', '-wago.bench.lifecycle'], ['BenchmarkCommandLifecycleDiagnostic/minimal-wasi/Imports']),
}


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--output', required=True, type=Path)
    p.add_argument('--binaries', type=Path, help='directory from reproduce-provider.sh')
    p.add_argument('--baseline', type=Path)
    p.add_argument('--candidate', type=Path)
    p.add_argument('--build-metadata', type=Path)
    p.add_argument('--samples', type=int, default=20)
    p.add_argument('--cases', nargs='+', choices=CASES, default=['focused', 'phases', 'host-owned'])
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
    env, prefix = settings(a.gomaxprocs, a.cpus)
    out = fresh_directory(a.output)
    meta = identity(env, binaries)
    meta.update(build=build, samples=a.samples, cases=a.cases, cpus=a.cpus, order='AB, BA alternating', status='running', records=[])
    metadata = out/'run.json'
    metadata.write_text(json.dumps(meta, indent=2)+'\n')
    leaves = {}
    for sample in range(1, a.samples+1):
        for label in (['baseline', 'candidate'] if sample % 2 else ['candidate', 'baseline']):
            for case in a.cases:
                args, required = CASES[case]
                command = prefix+[str(binaries[label]), '-test.run', '^$', '-test.count', '1', '-test.benchmem', *args]
                path = out/f'{sample:03d}-{label}-{case}.txt'
                record = run_process(command, path, env)
                parsed = rows(path.read_text())
                if not set(required) <= parsed.keys():
                    raise ValueError(f'missing required leaves for {case}: {required}')
                names = sorted(parsed)
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
