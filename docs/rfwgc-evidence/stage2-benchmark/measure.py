#!/usr/bin/env python3
"""Contention-qualified pinned archive comparison; serial rotating matrix with <=5% CPU admission and retries."""
import argparse, datetime, hashlib, io, json, os, pathlib, subprocess, tarfile, tempfile, time

BASE = '6ceb862219e64480c0503836b19ee1e02b209cc7'
PORT = 'd28005d48f42524f9bb8a6556d92c5b701c42cd9'
PKG = './src/core/compiler/backend/railshot/amd64'
HARNESS = PKG[2:] + '/gc_lifetime_bench_test.go'
ROOT = pathlib.Path(__file__).resolve().parents[3]
p = argparse.ArgumentParser()
p.add_argument('--out', type=pathlib.Path, required=True)
p.add_argument('--go-root', default='/home/dev/.local/share/mise/installs/go/1.27.1')
p.add_argument('--cpu', type=int, default=10)
p.add_argument('--repeats', type=int, default=6)
p.add_argument('--benchtime', default='500ms')
p.add_argument('--reuse-directory',type=pathlib.Path)
p.add_argument('--owner-pid',type=int)
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

def snapshot():
    cpus = {}
    for line in pathlib.Path('/proc/stat').read_text().splitlines():
        fields = line.split()
        if not fields[0].startswith('cpu'): continue
        values = list(map(int, fields[1:9]))
        cpus[fields[0]] = (sum(values), sum(values)-values[3]-values[4])
    return cpus, processes(), time.monotonic()

def observation(before, after, benchmark_pid=None):
    bc, bp, bt = before; ac, ap, at = after
    elapsed = at-bt
    total = ac['cpu'][0]-bc['cpu'][0]
    excluded_busy = ac[f'cpu{a.cpu}'][1]-bc[f'cpu{a.cpu}'][1] if benchmark_pid else 0
    excluded_total = ac[f'cpu{a.cpu}'][0]-bc[f'cpu{a.cpu}'][0] if benchmark_pid else 0
    external = max(0, ac['cpu'][1]-bc['cpu'][1]-excluded_busy) / (total-excluded_total) * 100
    siblings = [int(x) for x in pathlib.Path(f'/sys/devices/system/cpu/cpu{a.cpu}/topology/thread_siblings_list').read_text().strip().replace('-',',').split(',')]
    watched = [x for x in siblings if x != a.cpu or benchmark_pid is None]
    core = {str(x): 100*(ac[f'cpu{x}'][1]-bc[f'cpu{x}'][1])/max(1,ac[f'cpu{x}'][0]-bc[f'cpu{x}'][0]) for x in watched}
    busy = [(pid,name,round(100*(ticks-bp[pid][0])/os.sysconf('SC_CLK_TCK')/elapsed,2))
            for pid,(ticks,name) in ap.items() if pid in bp and pid not in (os.getpid(),benchmark_pid)
            and ticks-bp[pid][0] > 0]
    return {'utc':datetime.datetime.now(datetime.timezone.utc).isoformat(), 'seconds':elapsed,
            'external_host_pct':external, 'watched_cpu_busy_pct':core,
            'process_cpu_pct':busy, 'loadavg':pathlib.Path('/proc/loadavg').read_text().strip(),
            'noisy':external>5 or any(x>5 for x in core.values())}

last_progress=time.monotonic()
def quiet(label):
    stable=0
    while stable<3:
        if time.monotonic()-last_progress>300: raise RuntimeError("Quiet host blocked progress for >5 minutes; retained raw evidence")
        before=snapshot(); time.sleep(2); row=observation(before,snapshot())
        row['label']=label
        with (a.out/'admission.jsonl').open('a') as f: f.write(json.dumps(row)+'\n')
        stable=0 if row['noisy'] else stable+1
        if row['noisy']: print('Waiting for <=5% contention:', label, round(row['external_host_pct'],2), row['watched_cpu_busy_pct'],flush=True)

def timed(cmd, cwd, label):
    attempt=0
    while True:
        quiet(label)
        attempt+=1; observations=[]
        proc=subprocess.Popen(cmd,cwd=cwd,env=env,stdout=subprocess.PIPE,stderr=subprocess.STDOUT)
        before=snapshot(); utc_start=datetime.datetime.now(datetime.timezone.utc).isoformat()
        while True:
            done=False
            try: output,_=proc.communicate(timeout=1); done=True
            except subprocess.TimeoutExpired: pass
            after=snapshot()
            if after[2]-before[2]>=.25: observations.append(observation(before,after,proc.pid))
            before=after
            if done: break
        noisy=any(x['noisy'] for x in observations)
        with (a.out/'during-timing.jsonl').open('a') as f:
            f.write(json.dumps({'cell':label,'attempt':attempt,'accepted':not noisy,'utc_start':utc_start,
                    'utc_end':datetime.datetime.now(datetime.timezone.utc).isoformat(),'observations':observations})+'\n')
        if proc.returncode: raise RuntimeError(output.decode())
        if not noisy: return output.decode()
        log(f'rejected-{label}-attempt{attempt}.txt',output.decode())
        print('Rejected noisy invocation; waiting before retry:',label,flush=True)


import contextlib,signal
metadata={'base':BASE,'stage2':PORT,'utc_start':datetime.datetime.now(datetime.timezone.utc).isoformat(),
 'cpu':a.cpu,'siblings':pathlib.Path(f'/sys/devices/system/cpu/cpu{a.cpu}/topology/thread_siblings_list').read_text(),
 'repeats':a.repeats,'benchtime':a.benchtime,'go':run([go,'version']),
 'environment':{k:env.get(k,'<unset>') for k in ['GOMAXPROCS','GOFLAGS','GOGC','GOMEMLIMIT','GODEBUG','GOEXPERIMENT','CGO_ENABLED']},
 'method':'serial rotating revision order; one fixture Off+On per invocation; three 2s <=5% admission windows and all >=0.25s monitored intervals <=5% external-host and sibling; reused binaries from large-group attempt, same fixed environment; no concurrent builds/tests',
 'initial_processes':run(['ps','-eo','pid,ppid,etimes,pcpu,comm','--sort=-pcpu'])}
log('host.json',json.dumps(metadata,indent=2))
try:
 with (contextlib.nullcontext(str(a.reuse_directory)) if a.reuse_directory else tempfile.TemporaryDirectory(prefix='wago-stage2-benchmark-')) as td:
    tmp=pathlib.Path(td);env.update(GOCACHE=str(tmp/'gocache'),GOTMPDIR=str(tmp),GOWORK='off')
    runtime='src/wago/gc_lifetime_stage2_amd64_test.go'
    harness=run(['git','show',f'{PORT}:{runtime}']).encode()
    metadata['runtime_harness_sha256']=hashlib.sha256(harness).hexdigest()
    metadata['compile_harness_sha256']={}
    for name,ref in [('baseline',BASE),('stage2',PORT)]:
        tree=tmp/name
        if not a.reuse_directory:
            tree.mkdir()
            archive=subprocess.check_output(['git','archive',ref],cwd=ROOT)
            with tarfile.open(fileobj=io.BytesIO(archive)) as tf: tf.extractall(tree,filter='data')
            if name=='baseline':(tree/runtime).write_bytes(harness)
            for suite,pkg in [('runtime','./src/wago'),('compile',PKG)]:
                log(name+'-'+suite+'-build.txt',run([go,'test','-p=1','-c','-o',str(tmp/(name+'-'+suite+'.test')),pkg],tree))
        assert (tree/runtime).read_bytes()==harness
        metadata['compile_harness_sha256'][name]=hashlib.sha256((tree/HARNESS).read_bytes()).hexdigest()
    last_progress=time.monotonic()
    for repeat in range(a.repeats):
      kinds=['qualifying','nonqualifying','no-gc'];kinds=kinds[repeat%3:]+kinds[:repeat%3]
      for suite in (['runtime','compile'] if repeat%2==0 else ['compile','runtime']):
       for kind in kinds:
        for name in (['baseline','stage2'] if repeat%2==0 else ['stage2','baseline']):
            label=f'{repeat+1:02d}-{suite}-{name}-{kind}'
            bench=('^BenchmarkGCLifetimeRuntime$/^throughput$/^'+kind+'$' if suite=='runtime' else '^BenchmarkGCLifetimeCompile$/^'+kind+'$')
            cmd=['taskset','-c',str(a.cpu),str(tmp/(name+'-'+suite+'.test')),'-test.run=^$',
                 '-test.bench='+bench,'-test.benchmem','-test.count=1',f'-test.benchtime={a.benchtime}','-test.timeout=5m']
            print(label,flush=True)
            output=timed(cmd,tmp/name/('src/wago' if suite=='runtime' else PKG[2:]),label)
            log(label+'.txt','COMMAND: '+json.dumps(cmd)+'\n'+output)
            last_progress=time.monotonic()
finally:
 if a.owner_pid:
    os.kill(a.owner_pid,signal.SIGINT);os.kill(a.owner_pid,signal.SIGCONT)
    cleanup_deadline=time.monotonic()+10
    while a.reuse_directory.exists() and time.monotonic()<cleanup_deadline: time.sleep(.1)
    if a.reuse_directory.exists(): raise RuntimeError('Archive holder cleanup did not finish')
 metadata['utc_end']=datetime.datetime.now(datetime.timezone.utc).isoformat()
 log('host.json',json.dumps(metadata,indent=2))
 log('mirror-final.txt',run(['git','-C','/home/dev/Projects/slipstream','fetch','origin','master'])+run(['git','-C','/home/dev/Projects/slipstream','rev-parse','origin/master'])+run(['git','-C','/home/dev/Projects/slipstream','log','--oneline','db6014de2316f13dd08ea8618d614c28132f2688..origin/master']))
print('Completed',flush=True)
