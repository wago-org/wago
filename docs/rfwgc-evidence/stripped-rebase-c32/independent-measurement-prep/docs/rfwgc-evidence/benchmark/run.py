#!/usr/bin/env python3
"""Contention-qualified pinned archive comparison; serial rotating matrix, no admission waits."""
import argparse, datetime, hashlib, io, json, os, pathlib, subprocess, tarfile, tempfile, time

BASE = '9713878ab452848f7eb7549a90be94b938a898be'
PORT = '160ce3a893996d1cffae4e70a61678b5169dcdad'
PKG = './src/core/compiler/backend/railshot/amd64'
HARNESS = PKG[2:] + '/gc_lifetime_bench_test.go'
ROOT = pathlib.Path(__file__).resolve().parents[3]
p = argparse.ArgumentParser()
p.add_argument('--out', type=pathlib.Path, required=True)
p.add_argument('--go-root', default='/home/dev/.local/share/mise/installs/go/1.27.1')
p.add_argument('--cpu', type=int, default=6)
p.add_argument('--repeats', type=int, default=10)
p.add_argument('--benchtime', default='1s')
a = p.parse_args()
a.out = a.out.resolve(); a.out.mkdir(parents=True, exist_ok=True)
env = os.environ.copy()
env.update(GOROOT=a.go_root, GOTOOLCHAIN='local', GOMAXPROCS='1', GOFLAGS='', GOEXPERIMENT='')
env.pop('WAGO_EXPLAIN', None)
go = a.go_root + '/bin/go'
def run(cmd, cwd=ROOT):
    return subprocess.check_output(cmd, cwd=cwd, env=env, stderr=subprocess.STDOUT).decode()
def log(name, data):
    (a.out / name).write_text('\n'.join(line.rstrip() for line in data.splitlines())+'\n')
def processes():
    result = {}
    for d in pathlib.Path('/proc').iterdir():
        if not d.name.isdigit(): continue
        try:
            s = (d/'stat').read_text(); fields = s[s.rindex(')')+2:].split()
            result[int(d.name)] = (int(fields[11])+int(fields[12]), (d/'comm').read_text().strip())
        except (OSError, ValueError): pass
    return result

def timed(cmd, cwd, label):
    # Observe external CPU activity during calibration and measurement.
    # All samples are retained; timing is contention-qualified and exploratory.
    before = processes(); start = time.monotonic(); busy_seen = []
    utc_start = datetime.datetime.now(datetime.timezone.utc).isoformat()
    proc = subprocess.Popen(cmd, cwd=cwd, env=env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
    while True:
        done = False
        try:
            output, _ = proc.communicate(timeout=1)
            done = True
        except subprocess.TimeoutExpired: pass
        after = processes(); now = time.monotonic(); elapsed = now-start
        if elapsed >= .1:
            busy = [(pid,name,round((ticks-before[pid][0])/os.sysconf('SC_CLK_TCK')/elapsed,2))
                    for pid,(ticks,name) in after.items() if pid in before
                    and pid not in (os.getpid(),proc.pid)
                    and (ticks-before[pid][0])/os.sysconf('SC_CLK_TCK')/elapsed > .25]
            busy_seen.append({'utc': datetime.datetime.now(datetime.timezone.utc).isoformat(), 'processes_above_025_cpu': busy, 'loadavg': pathlib.Path('/proc/loadavg').read_text().strip()})
        before = after; start = now
        if done: break
    with (a.out/'during-timing.jsonl').open('a') as f:
        f.write(json.dumps({'cell':label,'utc_start':utc_start,'utc_end':datetime.datetime.now(datetime.timezone.utc).isoformat(),'observations':busy_seen})+'\n')
    if proc.returncode: raise RuntimeError(output.decode())
    return output.decode()


metadata = {
    'utc_start': datetime.datetime.now(datetime.timezone.utc).isoformat(), 'base': BASE, 'port': PORT,
    'runtime_environment': {k: env.get(k, '<unset; Go default>') for k in ['GOGC','GOMEMLIMIT','GODEBUG','GOMAXPROCS','GOFLAGS','GOEXPERIMENT','CGO_ENABLED']}, 'go': run([go,'version']).strip(), 'go_env': run([go,'env','-json']),
    'uname': run(['uname','-a']), 'lscpu': run(['lscpu']),
    'affinity': sorted(os.sched_getaffinity(0)), 'cpu': a.cpu,
    'smt_siblings': pathlib.Path(f'/sys/devices/system/cpu/cpu{a.cpu}/topology/thread_siblings_list').read_text().strip(),
    'governor': pathlib.Path(f'/sys/devices/system/cpu/cpu{a.cpu}/cpufreq/scaling_governor').read_text().strip(),
    'measurement_mode': 'contention-qualified exploratory; no rejection/retry', 'repeats': a.repeats, 'benchtime': a.benchtime, 'GOMAXPROCS': 1,
    'initial_processes': run(['ps','-eo','pid,ppid,etimes,pcpu,comm','--sort=-pcpu']),
}
log('host.json', json.dumps(metadata, indent=2)+'\n')
with tempfile.TemporaryDirectory(prefix='wago-lifetime-bench-') as tmp:
    tmp = pathlib.Path(tmp); trees = {}; binaries = {}
    harness = run(['git','show',f'{PORT}:{HARNESS}']).encode()
    metadata['harness_sha256'] = hashlib.sha256(harness).hexdigest()
    for name, ref in [('baseline',BASE), ('port',PORT)]:
        tree = tmp/name; tree.mkdir(); trees[name] = tree
        archive = subprocess.check_output(['git','archive',ref],cwd=ROOT)
        with tarfile.open(fileobj=io.BytesIO(archive)) as tf: tf.extractall(tree, filter='data')
        del archive
        if name == 'baseline': (tree/HARNESS).write_bytes(harness)
        binary = tmp/(name+'.test'); binaries[name] = binary
        print('Building', name, flush=True)
        log(name+'-build.txt', run([go,'test','-c','-o',str(binary),PKG], tree))
    # Exact original focused tests; bench binaries contain no extra diagnostic code.
    print('Focused validation', flush=True)
    log('focused.txt', run([str(binaries['port']),'-test.run=^TestGCLifetime','-test.v','-test.count=1'], trees['port']))
    # Supplementary 32-function diagnostic uses the original fixture and report.
    extra = trees['port']/PKG[2:]/'gc_lifetime_evidence_test.go'
    extra.write_text('''package amd64
import "testing"
func TestGCLifetimeBenchmarkEvidence(t *testing.T) {
 for _, kind := range []string{"qualifying", "nonqualifying", "no-gc"} {
  m := lifetimeFixture(t, kind, 32)
  for i := range m.Code {
   r := analyzeGCLifetime(m, i, gcLifetimeMaxRecords)
   t.Logf("kind=%s function=%d candidate=%t reason=%s sites=%d denominator-known=%t",kind,i,r.Candidate,r.Reason,r.Sites,r.DenominatorKnown)
   if r.Candidate != (kind == "qualifying") { t.Fatalf("unexpected candidate: %+v",r) }
  }
 }
}
''')
    log('sites.txt', run([go,'test',PKG,'-run','^TestGCLifetimeBenchmarkEvidence$','-v','-count=1'], trees['port']))
    extra.unlink()
    # Rotate all nine cells each round to distribute temporal drift. One process
    # per cell/repeat, one measured sample; Go calibration is also pinned.
    cells = [(kind,variant) for kind in ['qualifying','nonqualifying','no-gc']
             for variant in ['baseline-off','port-off','port-on']]
    for repeat in range(a.repeats):
        order = cells[repeat%len(cells):] + cells[:repeat%len(cells)]
        for kind,variant in order:
            tree_name = 'baseline' if variant=='baseline-off' else 'port'
            mode = variant.split('-')[-1]
            cmd = ['taskset','-c',str(a.cpu),str(binaries[tree_name]),'-test.run=^$',
                   f'-test.bench=^BenchmarkGCLifetimeCompile$/^{kind}$/^{mode}$',
                   '-test.benchmem','-test.count=1',f'-test.benchtime={a.benchtime}']
            print(f'Round {repeat+1}: {kind}/{variant}', flush=True)
            result = timed(cmd, trees[tree_name], f'{repeat+1:02d}-{kind}-{variant}')
            log(f'{repeat+1:02d}-{kind}-{variant}.txt', 'COMMAND: '+json.dumps(cmd)+'\n'+result)
        log(f'processes-{repeat+1:02d}.txt', run(['ps','-eo','pid,ppid,etimes,pcpu,comm','--sort=-pcpu']))
    metadata['utc_end'] = datetime.datetime.now(datetime.timezone.utc).isoformat()
    log('host.json', json.dumps(metadata,indent=2)+'\n')
print('Temporary archives, added diagnostic fixture and binaries removed.', flush=True)
