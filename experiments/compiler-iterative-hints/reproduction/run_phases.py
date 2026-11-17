#!/usr/bin/env python3
"""Official compile phases: one sample/fresh process, uniform memory profile.
Peak RSS at compiled includes adapter/instrumentation, excludes subsequent oracle.
Barrier timings are diagnostics, not uninstrumented headline timings.
"""
import argparse, hashlib, json, os, pathlib, random, select, subprocess, tempfile, time


def digest(path):
    return hashlib.sha256(pathlib.Path(path).read_bytes()).hexdigest()


class Channel:
    def __init__(self, proc, timeout):
        self.proc, self.timeout, self.pending = proc, timeout, b''

    def send(self, request):
        self.proc.stdin.write(json.dumps(request, separators=(',', ':')).encode() + b'\n')
        self.proc.stdin.flush()

    def receive(self, request_id):
        deadline = time.monotonic() + self.timeout
        while b'\n' not in self.pending:
            remaining = deadline - time.monotonic()
            if remaining <= 0 or not select.select([self.proc.stdout], [], [], remaining)[0]:
                raise TimeoutError('adapter response timeout')
            data = os.read(self.proc.stdout.fileno(), 65536)
            if not data:
                raise RuntimeError('adapter closed before response')
            self.pending += data
            if len(self.pending) > 32 * 1024 * 1024:
                raise RuntimeError('response exceeds controller budget')
        line, self.pending = self.pending.split(b'\n', 1)
        response = json.loads(line)
        if response.get('version') != 1 or response.get('id') != request_id:
            raise RuntimeError('response identity mismatch')
        return response


def snapshot(pid, stage):
    status = pathlib.Path(f'/proc/{pid}/status').read_text()
    values = {line.split(':')[0]: int(line.split()[1]) for line in status.splitlines()
              if line.startswith(('VmHWM:', 'VmRSS:'))}
    if set(values) != {'VmHWM', 'VmRSS'}:
        raise RuntimeError('Linux RSS fields unavailable')
    return {'stage': stage, 'unit': 'KiB', 'values': values}


def execute(binary, row, args):
    record = {'responses': [], 'rss_observations': [], 'acks': [], 'requests': [],
              'profile': 'memory', 'read_memstats': True,
              'rss_scope': 'absolute_process_peak_through_compile_before_oracle'}
    command = [binary] if args.cpu is None else ['taskset', '-c', str(args.cpu), binary]
    with tempfile.TemporaryFile(mode='w+') as stderr:
        proc = subprocess.Popen(command, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                stderr=stderr, env={**os.environ, 'GOMAXPROCS': str(args.gomaxprocs)})
        channel = Channel(proc, args.timeout)
        def send(request):
            record['requests'].append(request)
            channel.send(request)
        def receive(request_id, status):
            response = channel.receive(request_id)
            record['responses'].append(response)
            if response.get('status') != status:
                raise RuntimeError(f'expected {status}: {response}')
            return response
        try:
            for request in [
                {'version': 1, 'id': 1, 'method': 'describe'},
                {'version': 1, 'id': 2, 'method': 'prepare', 'prepare': {
                    'artifact': row['path'], 'artifact_sha256': row['artifact_sha256'],
                    'workload': row['workload'], 'profile': 'memory'}}]:
                send(request)
                receive(request['id'], 'ok')
                record['rss_observations'].append(snapshot(proc.pid, request['method']))
            send({'version': 1, 'id': 3, 'method': 'run', 'run': {
                'scenario': 'compile', 'samples': 1, 'warmup': 0,
                'operations': 1, 'phase_barriers': True}})
            for expected in ['before_compile', 'compiled', 'released']:
                response = receive(3, 'phase')
                if response.get('phase') != {'sample_index': 0, 'stage': expected}:
                    raise RuntimeError(f'unexpected phase: {response}')
                observation = snapshot(proc.pid, expected)
                record['rss_observations'].append(observation)
                if expected == 'compiled':
                    record['compile_peak_rss_kib'] = observation['values']['VmHWM']
                ack = {'version': 1, 'id': 3, 'method': 'continue'}
                # Snapshot before ACK: adapter remains blocked before verification.
                channel.send(ack)
                record['acks'].append(ack)
            response = receive(3, 'ok')
            samples = response.get('samples', [])
            if len(samples) != 1 or not samples[0].get('verified') or samples[0].get('operations') != 1:
                raise RuntimeError('sample verification failed')
            send({'version': 1, 'id': 4, 'method': 'close'})
            receive(4, 'ok')
            proc.stdin.close()
            if proc.wait(timeout=args.timeout) != 0:
                raise RuntimeError('adapter process failed')
            record['verified'] = True
        except Exception as error:
            record.update(verified=False, error=str(error))
        finally:
            if proc.poll() is None:
                proc.kill()
                proc.wait()
            stderr.seek(0)
            record.update(returncode=proc.returncode, stderr=stderr.read())
    return record


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--contracts', required=True)
    parser.add_argument('--baseline', required=True)
    parser.add_argument('--candidate', required=True)
    parser.add_argument('--output', required=True)
    parser.add_argument('--trials', type=int, default=1)
    parser.add_argument('--seed', type=int, default=145)
    parser.add_argument('--timeout', type=int, default=300)
    parser.add_argument('--only', default='')
    parser.add_argument('--cpu', type=int)
    parser.add_argument('--gomaxprocs', type=int, default=2)
    args = parser.parse_args()
    rows = json.loads(pathlib.Path(args.contracts).read_text())
    wanted = set(args.only.split(',')) if args.only else None
    rows = [row for row in rows if wanted is None or row['id'] in wanted]
    if not rows or args.trials < 1 or args.timeout < 1 or args.gomaxprocs < 1:
        parser.error('nonempty corpus and positive trials/timeout/gomaxprocs required')
    if wanted is not None and {row['id'] for row in rows} != wanted:
        parser.error('requested workload missing from contracts')
    for row in rows:
        if digest(row['path']) != row['artifact_sha256']:
            raise RuntimeError('artifact hash mismatch: ' + row['id'])
        workload = row['workload']
        if workload['abi'] != 'core' or workload['reset'] not in ['stateless', 'fresh_instance_per_sample']:
            raise RuntimeError('unsupported phase corpus contract: ' + row['id'])
    binaries = {'baseline': args.baseline, 'candidate': args.candidate}
    hashes = {variant: digest(binary) for variant, binary in binaries.items()}
    contracts_hash = digest(args.contracts)
    rng = random.Random(args.seed)
    failures = 0
    with open(args.output, 'x') as output:
        for trial in range(args.trials):
            rng.shuffle(rows)
            for row in rows:
                variants = list(binaries)
                rng.shuffle(variants)
                for variant in variants:
                    record = {'trial': trial, 'variant': variant, 'id': row['id'],
                              'artifact_sha256': row['artifact_sha256'],
                              'contract_sha256': row['contract_sha256'],
                              'binary_sha256': hashes[variant], 'contracts_file_sha256': contracts_hash,
                              'gomaxprocs': args.gomaxprocs, 'cpu': args.cpu, 'seed': args.seed}
                    try:
                        record.update(execute(binaries[variant], row, args))
                    except Exception as error:
                        record.update(verified=False, error=str(error))
                    failures += not record['verified']
                    output.write(json.dumps(record, separators=(',', ':')) + '\n')
                    output.flush()
    if failures:
        raise SystemExit(f'{failures} failed samples; do not exclude from acceptance denominator')


if __name__ == '__main__':
    main()
