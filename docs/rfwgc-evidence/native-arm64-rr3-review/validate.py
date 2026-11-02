import os, subprocess, json, pathlib, hashlib
out=pathlib.Path(__file__).resolve().parent
root=pathlib.Path('/tmp/rr3-review-6c71/harness')
source=root.parent/'source'
go='/home/dev/.local/share/mise/installs/go/1.27.1/bin/go'
env=dict(os.environ, GOROOT=str(pathlib.Path(go).parent.parent),GOTOOLCHAIN='local',GOENV='off',GOWORK='off',PYTHONDONTWRITEBYTECODE='1')
rows=[]
def run(label,cmd,cwd=root,timeout=180):
    row=dict(label=label,command=cmd,cwd=str(cwd),timeout_seconds=timeout,exit=None)
    rows.append(row)
    (out/'commands.json').write_text(json.dumps(rows,indent=2)+'\n')
    with (out/(label+'.log')).open('w') as log:
        try: row['exit']=subprocess.run(cmd,cwd=cwd,env=env,stdout=log,stderr=subprocess.STDOUT,timeout=timeout).returncode
        except subprocess.TimeoutExpired: row['exit']=124
    (out/'commands.json').write_text(json.dumps(rows,indent=2)+'\n')
    print(label,row['exit'],flush=True)
run('toolchain',[go,'env','-json','GOVERSION','GOROOT','GOHOSTOS','GOHOSTARCH'])
run('actionlint',[go,'run','github.com/rhysd/actionlint/cmd/actionlint@v1.7.7','.github/workflows/ci.yml'])
for mode in ['ordinary','wagodebug','wago_regalloccheck']:
    run(mode+'-policy-planner',[go,'test',* ([] if mode=='ordinary' else ['-tags='+mode]),'./tests/integration/cipolicy','./tests/tools/ci-plan','-count=1','-timeout=120s'])
run('refusal-census',['python3','-m','unittest','discover','-s','docs/rfwgc-evidence/native-arm64-rr3','-p','test_harness.py','-v'])
plan=json.loads((root/'docs/rfwgc-evidence/native-arm64-rr3/plan.json').read_text())
checks={p:hashlib.sha256((source/p).read_bytes()).hexdigest()==h for p,h in plan['probe_sha256'].items()}
checks['bootstrap']=hashlib.sha256((source/'scripts/bootstrap-wabt.sh').read_bytes()).hexdigest()==plan['bootstrap_sha256']
checks['timer']=hashlib.sha256((root/'docs/rfwgc-evidence/native-arm64-rr3/timer-capability.go.txt').read_bytes()).hexdigest()==plan['timer_control_sha256']
(out/'hashes.json').write_text(json.dumps(checks,indent=2)+'\n')
assert all(checks.values())
assert all(r['exit']==0 for r in rows)
