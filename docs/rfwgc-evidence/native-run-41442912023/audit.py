#!/usr/bin/env python3
"""Offline receipt audit; reads Git objects and archives, never runs target code."""
import collections, gzip, hashlib, json, struct, subprocess, zipfile
from pathlib import Path
P = Path(__file__).resolve().parent
SOURCE = '5847df30f1af15d10b743d90a24b1429b290d358'
HARNESS = '6c71c4d6747456578f55928e900e36bb2d1449a8'
REVIEW = '9f8e4cad8d2b6d5ce262967e48f275a8bb9ae872'
def sha(b): return hashlib.sha256(b).hexdigest()
def git(*args): return subprocess.check_output(['git', *args])
def save(name, data):
    f=P/name; f.parent.mkdir(parents=True,exist_ok=True); f.write_bytes(data)
def js(name, data): save(name,(json.dumps(data,indent=2)+'\n').encode())
def blob(pin,path): return git('show',pin+':'+path)
plan=json.loads(blob(HARNESS,'docs/rfwgc-evidence/native-arm64-rr3/plan.json'))
for pin,path,out in [
 (HARNESS,'docs/rfwgc-evidence/native-arm64-rr3/plan.json','plan.json'),
 (HARNESS,'docs/rfwgc-evidence/native-arm64-rr3/run.py','harness-run.py.txt'),
 (HARNESS,'.github/workflows/ci.yml','workflow.yml.txt'),
 (REVIEW,'docs/rfwgc-evidence/native-arm64-rr3-review/census-audit.json','prior-census-audit.json'),
 (SOURCE,'src/core/compiler/backend/railshot/arm64/diagnostics_test.go','diagnostics_test.go.txt'),
 (SOURCE,'src/core/compiler/backend/railshot/arm64/diagnostics_enabled.go','diagnostics_enabled.go.txt'),
 (SOURCE,'src/core/compiler/backend/railshot/arm64/register_call_slot_arm64_test.go','register_call_slot_arm64_test.go.txt')]:
    save('inputs/'+out,blob(pin,path))
# Keep exact GitHub compressed logs plus byte-exact expanded aggregate job logs.
with zipfile.ZipFile(P/'run-logs.zip') as z:
    assert z.testzip() is None
    mapping={'ordinary':125268925843,'wagodebug':125268926227,'wago_regalloccheck':125268926269,'summary':125270222081}
    for mode,j in mapping.items():
        suffix=('Native Linux ARM64 RR3 qualification summary.txt' if mode=='summary' else 'Pinned native Linux ARM64 RR3 _ '+mode+'.txt')
        names=[n for n in z.namelist() if '/' not in n and n.endswith(suffix)]
        assert len(names)==1
        b=z.read(names[0]);save(f'job-{j}.log',b);save(f'job-{j}.log.gz',gzip.compress(b,mtime=0))
arts=json.loads((P/'artifacts.json').read_text())['artifacts']
audit={'source':SOURCE,'harness':HARNESS,'review':REVIEW,'run':41442912023,'qualified':False,'modes':{}}
for art in arts:
    arc=P/(art['name']+'.zip'); data=arc.read_bytes()
    assert 'sha256:'+sha(data)==art['digest']
    mode=art['name'].removeprefix('native-arm64-rr3-5847df30f-').removesuffix('-1')
    prefix='receipts-'+mode+'/'
    with zipfile.ZipFile(arc) as z:
        assert z.testzip() is None
        def read(n): return z.read(prefix+n)
        def obj(n): return json.loads(read(n))
        summary=obj('summary.json');commands=obj('commands.json');host=obj('host.json');env=obj('go-env.log')
        assert summary['source']==SOURCE and summary['harness']==HARNESS
        assert summary['native_execution']=='RUN' and summary['qualified'] is False
        assert all(c['exit']==0 for c in commands)
        assert host['runner_arch']=='ARM64' and host['uname'][4]=='aarch64' and host['python_elf_machine']==183
        assert all(env[k]==v for k,v in {'GOOS':'linux','GOARCH':'arm64','GOHOSTOS':'linux','GOHOSTARCH':'arm64','GOVERSION':'go1.27.1','CGO_ENABLED':'1'}.items())
        integrity={}
        for label,pin in [('source',SOURCE),('harness',HARNESS)]:
            before=obj(label+'-before-manifest.json');after=obj(label+'-after-manifest.json')
            assert before==after and summary[label+'_unchanged']
            assert read(label+'-head.log').decode().strip()==pin
            tree=git('ls-tree','-rz',pin)
            for phase in ['before','after']:
                assert read(label+'-'+phase+'-tree.log')==tree
                assert not read(label+'-'+phase+'-status.log').strip()
                assert not read(label+'-'+phase+'-diff.log').strip()
            integrity[label]={'before_after_equal':True,'git_tree_matches_pin':True,'tracked_blob_count':len(before)}
        probes={}
        for path,digest in plan['probe_sha256'].items():
            assert sha(blob(SOURCE,path))==digest==obj('source-before-manifest.json')[path]
            probes[path]=digest
        timer=read('timer-capability.go')
        assert sha(timer)==plan['timer_control_sha256']==sha(blob(HARNESS,'docs/rfwgc-evidence/native-arm64-rr3/timer-capability.go.txt'))
        assert sha(blob(SOURCE,'scripts/bootstrap-wabt.sh'))==plan['bootstrap_sha256']
        binaries={}
        for name,identity in summary['binaries'].items():
            b=read(name+'.test');assert sha(b)==identity['sha256'] and b[:4]==b'\x7fELF' and struct.unpack('<H',b[18:20])[0]==183
            binaries[name]=identity
        assert struct.unpack('<H',read('timer-control')[18:20])[0]==183
        gates={}; outcomes={}
        for gate,census in summary['census'].items():
            suite=plan['suites'][gate.split('-')[-1]];expected=suite['tests']
            assert sorted(read(gate+'-list.log').decode().splitlines())==expected
            events=[json.loads(l) for l in read(gate+'.log').decode().splitlines()]
            assert all(e.get('Package')==suite['package'] for e in events)
            starts=collections.Counter(e['Test'] for e in events if e.get('Test') and e['Action']=='run')
            passes=collections.Counter(e['Test'] for e in events if e.get('Test') and e['Action']=='pass')
            fails=[e for e in events if e['Action']=='fail'];skips=[e for e in events if e['Action']=='skip']
            topstart={k:v for k,v in starts.items() if '/' not in k};toppass={k:v for k,v in passes.items() if '/' not in k}
            assert topstart==dict(collections.Counter(expected))==census['started']
            assert toppass==census['passed'] and dict(passes)==census['all_test_passes']
            assert not fails and census['exit']==0
            package=[e['Action'] for e in events if not e.get('Test') and e['Action'] in ('pass','fail','skip')]
            assert package==['pass']
            if gate=='backend-backend':
                assert len(skips)==1 and skips[0]['Test']=='TestRegisterCallOverlappingArgumentSlotARM64'
                assert starts-passes==collections.Counter({'TestRegisterCallOverlappingArgumentSlotARM64':1})
                assert census['ok'] is False
            else:
                assert not skips and starts==passes and all(n==1 for n in passes.values()) and census['ok']
            gates[gate]={'expected':len(expected),'started':len(topstart),'passed':len(toppass),'fail':len(fails),'skip':len(skips),'not_run':sorted(set(expected)-set(topstart)),'exit':0,'census_ok':census['ok'],'all_pass_events':len(passes),'skipped_names':[e['Test'] for e in skips]}
            outcomes[gate]={k:'PASS' for k in passes if k.startswith(('TestRRRecheck','TestRRRetirement','TestSharedEntryGuardCancellationAndGC'))}
        assert len(gates)==6
        timer_rows=[dict(item.split('=',1) for item in line.split() if '=' in item) for line in read('timer-control.log').decode().splitlines()]
        assert {(r['notify'],r['signo'],r['errno']) for r in timer_rows}=={(str(n),str(s),'0') for n in [1,0,4] for s in [35,64]} and len(timer_rows)==6
        # Human-readable receipts are exact archive bytes. Full manifests/binaries remain in raw ZIP.
        for n in z.namelist():
            if n.startswith(prefix) and (n.endswith('.json') or n.endswith('.log') or n.endswith('.txt')) and '-manifest.' not in n and '-tree.' not in n:
                save(mode+'/'+n.removeprefix(prefix),z.read(n))
        audit['modes'][mode]={'artifact_sha256':sha(data),'artifact_bytes':len(data),'setup':'PASS','native_execution':'RUN','qualified':False,'driver_exit':1,'integrity':integrity,'gates':gates,'original_probe_outcomes':outcomes,'probe_sha256':probes,'timer_control_sha256':sha(timer),'timer_control_rows':timer_rows,'binaries':binaries,'go_executable':summary['go_executable'],'wabt_executable':summary['wabt_executable'],'command_count':len(commands),'nonzero_or_pending_commands':[],'first_blocking_line':json.loads(read('backend-backend.log').decode().splitlines()[3])['Output'].strip(),'phase':'backend execution census: diagnostics prerequisite skip'}
js('audit.json',audit)
print(json.dumps({m:{'gates':v['gates'],'integrity':v['integrity'],'first_blocking_line':v['first_blocking_line']} for m,v in audit['modes'].items()},indent=2))
