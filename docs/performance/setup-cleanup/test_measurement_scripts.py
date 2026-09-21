#!/usr/bin/env python3
import importlib.util
import io
import sys
import threading
import time
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest
from unittest import mock

HERE = Path(__file__).resolve().parent


def module(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    result = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(result)
    return result


class Scripts(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix='wago script tests ')
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)/'checkout with spaces'
        self.scripts = self.root/'docs/performance/setup-cleanup'
        self.scripts.mkdir(parents=True)
        (self.root/'bench/suite').mkdir(parents=True)
        for name in ['measurement.py', 'profile-diagnostics.py', 'diagnose-command.sh', 'diagnose-memory-workers.sh']:
            shutil.copy2(HERE/name, self.scripts/name)
        (self.scripts/'continuation').mkdir()
        for name in ['run-wasi-pairs.py', 'summarize-wasi.py']:
            shutil.copy2(HERE/'continuation'/name, self.scripts/'continuation'/name)
        subprocess.run(['git', 'init', '-q', str(self.root)], check=True)
        subprocess.run(['git', '-C', str(self.root), '-c', 'user.name=Fixture', '-c', 'user.email=fixture@example.invalid', 'commit', '-q', '--allow-empty', '-m', 'fixture'], check=True)
        self.bins = Path(self.tmp.name)/'nondefault work with spaces'
        self.bins.mkdir()
        code = '''#!/usr/bin/env python3
import os,sys
from pathlib import Path
args=sys.argv[1:]
if os.environ.get('FIXTURE_FAIL'): sys.exit(3)
if os.environ.get('FIXTURE_EMPTY'):
 print('PASS');sys.exit()
selector=args[args.index('-test.bench')+1]
name=selector.replace('^','').replace('$','').replace('\\\\','')
for flag in ['-test.cpuprofile','-test.memprofile']:
 if flag in args: Path(args[args.index(flag)+1]).write_text('profile fixture')
extra=''
if 'WorkerLifecycle' in name:
 callers=int(name.split('callers=')[1].split('/')[0])
 extra=f' {callers} callers {os.environ["GOMAXPROCS"]} gomaxprocs 0 requested-workers 4 phase-input-workers 4 validation-worker-limit 4 backend-worker-limit'
print(f'{name}-16 2 100 ns/op 32 B/op 1 allocs/op'+extra)
print('PASS')
'''
        for label in ['baseline', 'candidate']:
            p = self.bins/f'wasi-{label}.test'
            p.write_text(code); p.chmod(0o755)
        import hashlib
        build = {'provider_base': 'fixture', 'binaries': {label: {'sha256': hashlib.sha256((self.bins/f'wasi-{label}.test').read_bytes()).hexdigest()} for label in ['baseline', 'candidate']}}
        (self.bins/'build.json').write_text(json.dumps(build))
        self.out = Path(self.tmp.name)/'new output'

    def paired(self, extra=(), env=None):
        return subprocess.run(['python3', str(self.scripts/'continuation/run-wasi-pairs.py'), '--binaries', str(self.bins), '--output', str(self.out), '--samples', '2', '--cases', 'smoke', '--cpus', '', *extra], cwd='/', env=env, capture_output=True, text=True)

    def test_normal_spaces_nondefault_and_summary(self):
        r = self.paired(); self.assertEqual(r.returncode, 0, r.stderr)
        m = json.loads((self.out/'run.json').read_text())
        self.assertEqual([(r['sample'],r['label']) for r in m['records']], [(1,'baseline'),(1,'candidate'),(2,'candidate'),(2,'baseline')])
        summary = module('summary', HERE/'continuation/summarize-wasi.py')
        self.assertEqual(len(summary.validate(self.out)), 2)
        m['records'].append(m['records'][0]); (self.out/'run.json').write_text(json.dumps(m))
        with self.assertRaisesRegex(ValueError, 'duplicate'):
            summary.validate(self.out)

    def test_existing_output_and_missing_binary(self):
        self.out.mkdir(); (self.out/'evidence.txt').write_text('keep')
        self.assertNotEqual(self.paired().returncode, 0)
        self.assertEqual((self.out/'evidence.txt').read_text(), 'keep')
        (self.bins/'wasi-candidate.test').unlink()
        self.assertIn('missing executable', self.paired().stderr)

    def test_empty_result_and_failed_process(self):
        for key in ['FIXTURE_EMPTY', 'FIXTURE_FAIL']:
            self.out = Path(self.tmp.name)/key
            r = self.paired(env=dict(os.environ, **{key:'1'}))
            self.assertNotEqual(r.returncode, 0)
            self.assertNotEqual(json.loads((self.out/'run.json').read_text())['status'], 'complete')

    def test_missing_and_mixed_samples(self):
        self.assertEqual(self.paired().returncode, 0)
        summary = module('summary', HERE/'continuation/summarize-wasi.py')
        original = json.loads((self.out/'run.json').read_text())
        for mode in ['missing', 'mixed']:
            m=json.loads(json.dumps(original))
            if mode=='missing': m['records'].pop()
            else: m['records'][0]['environment']['GOGC']='50'
            (self.out/'run.json').write_text(json.dumps(m))
            with self.assertRaises(ValueError): summary.validate(self.out)

    def test_worker_filter_and_profiles(self):
        r = subprocess.run(['bash', str(self.scripts/'diagnose-memory-workers.sh'), '--binary', str(self.bins/'wasi-candidate.test'), '--output', str(self.out), '--gomaxprocs', '8', '--callers', '8', '--workers-only', '--cpus', ''], cwd='/', capture_output=True, text=True)
        self.assertEqual(r.returncode, 0, r.stderr)
        m=json.loads((self.out/'run.json').read_text())
        self.assertEqual(len(m['profiles']),2)
        self.assertEqual(m['profiles'][0]['filter'], '^BenchmarkWorkerLifecycleDiagnostic$/^json\\-as$/^callers=8$/^full$/^public$/^requested=0$')
        self.assertEqual(m['profiles'][1]['metrics']['callers'],8)

    def test_profile_empty_is_failure(self):
        r=subprocess.run(['bash',str(self.scripts/'diagnose-memory-workers.sh'),'--binary',str(self.bins/'wasi-candidate.test'),'--output',str(self.out),'--workers-only','--cpus',''],env=dict(os.environ,FIXTURE_EMPTY='1'),capture_output=True,text=True)
        self.assertNotEqual(r.returncode,0)

    def test_memory_configuration_and_checkpoints(self):
        summary = module('memory_summary', HERE/'memory-review/summarize.py')
        env = dict(GOMAXPROCS='16', GOGC='100', GOMEMLIMIT='off', GODEBUG='', WAGO_BOUNDS='signals')
        meta = dict(configuration=dict(profile=False, legacy=False, epochs=1), environment=env)
        phases = ['startup', 'ready', 'setup', 'warm', 'epoch1', 'released', 'post']
        record = dict(environment=dict(env), result=dict(Points=[dict(Phase=p, external=dict(index=i)) for i,p in enumerate(phases)]))
        summary.validate_record(record, meta)

        for key in env:
            bad = json.loads(json.dumps(record)); bad['environment'][key] = 'changed'
            with self.assertRaisesRegex(ValueError, 'configuration'):
                summary.validate_record(bad, meta)
        for mode in ['missing', 'duplicate']:
            bad = json.loads(json.dumps(record))
            if mode == 'missing': bad['result']['Points'].pop()
            else: bad['result']['Points'].append(bad['result']['Points'][0])
            with self.assertRaisesRegex(ValueError, 'checkpoints'):
                summary.validate_record(bad, meta)
        meta['configuration']['profile'] = True
        record['environment']['GODEBUG'] = 'memprofilerate=1,gctrace=1,inittrace=1'
        summary.validate_record(record, meta)

    def test_historical_memory_corpus_context(self):
        runner = module('memory_runner', HERE/'memory-review/run-memory.py')
        with mock.patch.object(runner.subprocess, 'Popen', side_effect=OSError('fixture stop')) as start:
            with self.assertRaises(OSError):
                runner.one(self.bins/'wasi-baseline.test', Path(self.tmp.name)/'legacy.txt', {}, [], 'tinyxml2', 'raw', 'scavenge', 1000, 1, legacy=True)
        command = start.call_args.args[0]
        self.assertEqual(command[command.index('-wago.corpus')+1], 'cjson,tinyxml2')
        self.assertEqual(command[command.index('-test.run')+1], '^TestWASIResources$/^tinyxml2$')

    def test_memory_zero_warmup_and_host_corpus(self):
        runner = module('memory_runner_shapes', HERE/'memory-review/run-memory.py')
        for workload in ['startup', 'host1024']:
            with mock.patch.object(runner.subprocess, 'Popen', side_effect=OSError('fixture stop')) as start:
                with self.assertRaises(OSError):
                    runner.one(self.bins/'wasi-baseline.test', Path(self.tmp.name)/(workload+'.txt'), {}, [], workload, 'raw', 'normal', 1, 1, warm=0)
            command = start.call_args.args[0]
            self.assertEqual(command[command.index('-wago.review.warm')+1], '0')
            self.assertEqual(command[command.index('-wago.corpus')+1], 'cjson')

    def test_full_screen_rejects_missing_leaves(self):
        result = self.paired(extra=['--cases', 'full'])
        self.assertNotEqual(result.returncode, 0)
        self.assertNotEqual(json.loads((self.out/'run.json').read_text())['status'], 'complete')

    def test_exact_selection(self):
        selection = Path(self.tmp.name)/'selection.json'
        name = 'BenchmarkCommandLifecycleDiagnostic/minimal-wasi/Imports'
        selection.write_text(json.dumps({'benchmarks': [name]}))
        result = self.paired(extra=['--selection', str(selection)])
        self.assertEqual(result.returncode, 0, result.stderr)
        meta = json.loads((self.out/'run.json').read_text())
        self.assertEqual(len(meta['records']), 4)
        self.assertTrue(all(r['leaves'] == [name] for r in meta['records']))
        runner = module('selected_runner', HERE/'continuation/run-wasi-pairs.py')
        selection.write_text(json.dumps({'benchmarks': [name, name]}))
        with self.assertRaisesRegex(ValueError, 'duplicate'):
            runner.selected_cases(selection)
        selection.write_text(json.dumps({'benchmarks': ['BenchmarkBad//leaf']}))
        with self.assertRaisesRegex(ValueError, 'valid benchmark'):
            runner.selected_cases(selection)

    def test_temperature_guard_and_process_stop(self):
        measurement = module('thermal_measurement', HERE/'measurement.py')
        sensor = Path(self.tmp.name)/'temperature'
        sensor.write_text('50000')
        guard = measurement.TemperatureGuard(sensor, 80, 65, 0, 2, io.StringIO())
        self.assertLess(guard.wait(), .1)
        self.assertEqual(guard.check(), 50)
        sensor.write_text('80000')
        with self.assertRaisesRegex(ValueError, 'temperature stop'):
            guard.check()
        sensor.write_text('50000')
        pidfile = Path(self.tmp.name)/'fixture.pid'
        command = [sys.executable, '-c', 'import os,time,pathlib; pathlib.Path('+repr(str(pidfile))+').write_text(str(os.getpid())); time.sleep(30)']
        timer = threading.Timer(.2, lambda: sensor.write_text('81000'))
        timer.start()
        try:
            with self.assertRaisesRegex(ValueError, 'temperature stop'):
                measurement.run_process(command, Path(self.tmp.name)/'thermal.txt', dict(os.environ), thermal=guard)
        finally:
            timer.join()
        with self.assertRaises(ProcessLookupError):
            os.kill(int(pidfile.read_text()), 0)

    def test_provider_patch_argument_validation(self):
        script = HERE/'continuation/reproduce-provider.py'
        missing = Path(self.tmp.name)/'missing patch'
        command = ['python3', str(script), str(self.out), '--production-patch', str(missing)]
        result = subprocess.run(command, capture_output=True, text=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('missing provider patch', result.stderr)
        self.assertFalse(self.out.exists())
        missing.write_text('fixture')
        result = subprocess.run(command+['--comparison', 'snapshot'], capture_output=True, text=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('requires --comparison provider', result.stderr)
        self.assertFalse(self.out.exists())


if __name__ == '__main__':
    unittest.main()
