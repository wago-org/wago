import os, sys, time, subprocess, signal, json
from pathlib import Path
root=Path(__file__).parent
name,cwd,*cmd=sys.argv[1:]
pause_celsius=70
sensor=Path('/sys/class/hwmon/hwmon8/temp1_input')
def temp(): return int(sensor.read_text())/1000
while temp()>pause_celsius: time.sleep(1)
env=dict(os.environ,GOMAXPROCS='1',GOFLAGS='-p=1',GOWORK=str(root/'build.work'),GOTOOLCHAIN='local',WAGO_BOUNDS='')
env.pop('GOROOT',None)
env['PATH']='/home/jtenner/.local/share/mise/installs/github-web-assembly-wabt/1.0.41/bin:'+env['PATH']
peak=temp()
wall_clock_tests=False
with (root/'checks'/(name+'.log')).open('w') as out:
    p=subprocess.Popen(['taskset','-c','0',*cmd],cwd=cwd,env=env,stdout=out,stderr=subprocess.STDOUT,start_new_session=True)
    try:
        while p.poll() is None:
            peak=max(peak,temp())
            if peak>=75: raise RuntimeError('CPU temperature reached stop threshold')
            if cmd == ['just', 'lint'] and not wall_clock_tests:
                wall_clock_tests='node --test scripts/fuzz-engine-state.test.mjs' in (root/'checks'/(name+'.log')).read_text()
            time.sleep(.05)
            if p.poll() is None and not wall_clock_tests:
                os.killpg(p.pid,signal.SIGSTOP)
                time.sleep(.15)
                while temp()>pause_celsius: time.sleep(.5)
                os.killpg(p.pid,signal.SIGCONT)
    finally:
        if p.poll() is None:
            os.killpg(p.pid,signal.SIGCONT)
            os.killpg(p.pid,signal.SIGTERM)
            try: p.wait(timeout=5)
            except subprocess.TimeoutExpired:
                os.killpg(p.pid,signal.SIGKILL); p.wait()
result={'name':name,'command':cmd,'exit':p.returncode,'max_celsius':peak}
(root/'checks'/(name+'.json')).write_text(json.dumps(result,indent=2)+'\n')
print(json.dumps(result))
print('\n'.join((root/'checks'/(name+'.log')).read_text().splitlines()[-25:]))
sys.exit(p.returncode)
