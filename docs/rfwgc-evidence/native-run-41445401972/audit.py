#!/usr/bin/env python3
"""Offline receipt audit; reads Git objects and archives, never runs target code."""
import collections, gzip, hashlib, json, re, struct, subprocess, zipfile
from pathlib import Path
P = Path(__file__).resolve().parent
SOURCE = '5847df30f1af15d10b743d90a24b1429b290d358'
HARNESS = '4af18b3a2484b901e11f7347adab8e547176dbc6'
REVIEW = 'ccfa39bd9604830cd10f73d5d764c2fd54687542'
def sha(b): return hashlib.sha256(b).hexdigest()
def git(*args): return subprocess.check_output(['git', *args])
def save(name, data):
    f=P/name; f.parent.mkdir(parents=True,exist_ok=True); f.write_bytes(data)
def js(name, data): save(name,(json.dumps(data,indent=2)+'\n').encode())
def blob(pin,path): return git('show',pin+':'+path)
plan=json.loads(blob(HARNESS,'docs/rfwgc-evidence/native-arm64-rr3/plan.json'))
# Independently hash every pinned Git blob; no target binary execution.
pinned_manifests={}
for pin in [SOURCE,HARNESS]:
    entries=[]
    for row in git('ls-tree','-rz',pin).split(b'\0'):
        if not row: continue
        header,path=row.split(b'\t',1); mode,kind,oid=header.split()
        if kind==b'blob': entries.append((path.decode(),oid))
    proc=subprocess.Popen(['git','cat-file','--batch'],stdin=subprocess.PIPE,stdout=subprocess.PIPE)
    raw,_=proc.communicate(b'\n'.join(oid for _,oid in entries)+b'\n')
    assert proc.returncode==0
    offset=0; manifest={}
    for path,oid in entries:
        end=raw.index(b'\n',offset); fields=raw[offset:end].split(); size=int(fields[2])
        assert fields[:2]==[oid,b'blob']
        data=raw[end+1:end+1+size]; assert len(data)==size
        manifest[path]=sha(data); offset=end+size+2
    assert offset==len(raw)
    pinned_manifests[pin]=manifest
run=json.loads((P/'run.json').read_text())
assert run['id']==41445401972 and run['head_sha']==HARNESS and run['run_attempt']==1
assert run['status']=='completed' and run['conclusion']=='success' and run['event']=='workflow_dispatch'
jobs=json.loads((P/'jobs.json').read_text())
assert jobs['total_count']==len(jobs['jobs'])==27
assert collections.Counter(j['conclusion'] for j in jobs['jobs'])=={'success':4,'skipped':23}
for path in ['.github/workflows/ci.yml','docs/rfwgc-evidence/native-arm64-rr3/plan.json','docs/rfwgc-evidence/native-arm64-rr3/timer-capability.go.txt']:
    assert blob(HARNESS,path)==blob('6c71c4d6747456578f55928e900e36bb2d1449a8',path)

for pin,path,out in [
 (REVIEW,'docs/rfwgc-evidence/native-arm64-rr3-diagnostics-recheck/README.md','activation-review.md'),
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
    mapping={'ordinary':125277129394,'wagodebug':125277130068,'wago_regalloccheck':125277128978,'summary':125278456285}
    for mode,j in mapping.items():
        suffix=('Native Linux ARM64 RR3 qualification summary.txt' if mode=='summary' else 'Pinned native Linux ARM64 RR3 _ '+mode+'.txt')
        names=[n for n in z.namelist() if '/' not in n and n.endswith(suffix)]
        assert len(names)==1
        b=z.read(names[0]);save(f'job-{j}.log',b);save(f'job-{j}.log.gz',gzip.compress(b,mtime=0))
arts=json.loads((P/'artifacts.json').read_text())['artifacts']
assert len(arts)==3 and len({a['name'] for a in arts})==3
audit={'source':SOURCE,'harness':HARNESS,'review':REVIEW,'run':41445401972,'qualified':True,'modes':{}}
for art in arts:
    arc=P/(art['name']+'.zip'); data=arc.read_bytes()
    assert 'sha256:'+sha(data)==art['digest'] and len(data)==art['size_in_bytes']
    assert art['workflow_run']['id']==41445401972 and art['workflow_run']['head_sha']==HARNESS and not art['expired']
    mode=art['name'].removeprefix('native-arm64-rr3-5847df30f-').removesuffix('-1')
    prefix='receipts-'+mode+'/'
    with zipfile.ZipFile(arc) as z:
        assert z.testzip() is None
        def read(n): return z.read(prefix+n)
        def obj(n): return json.loads(read(n))
        summary=obj('summary.json');commands=obj('commands.json');host=obj('host.json');env=obj('go-env.log')
        job=next(j for j in jobs['jobs'] if j['name']=='Pinned native Linux ARM64 RR3 / '+mode)
        assert job['conclusion']=='success' and all(s['conclusion']=='success' for s in job['steps'])
        log=(P/f"job-{job['id']}.log").read_text()
        assert 'Image: ubuntu-24.04-arm' in log
        assert f'QUALIFICATION_SOURCE: {SOURCE}' in log and f'QUALIFICATION_DRIVER: {HARNESS}' in log
        lines=[re.sub(r'^\S+Z ', '', line) for line in log.splitlines()]
        start=next(i for i,line in enumerate(lines) if line=='{' and i+1<len(lines) and '"qualified": true' in lines[i+1])
        logged_summary,_=json.JSONDecoder().raw_decode('\n'.join(lines[start:]))
        assert logged_summary==summary
        assert art['digest'].removeprefix('sha256:') in log

        assert summary['source']==SOURCE and summary['harness']==HARNESS
        assert summary['native_execution']=='RUN' and summary['qualified'] is True
        assert len(commands)==35 and all(c['exit']==0 for c in commands)
        bylabel={c['label']:c for c in commands}
        assert len(bylabel)==35
        build_commands={}
        for group,package in [('wago','./src/wago'),('wago-race','./src/wago'),('runtime','./src/core/runtime'),('backend','./src/core/compiler/backend/railshot/arm64')]:
            tags=[] if mode=='ordinary' else [mode]
            if group=='backend': tags.append('wago_codegenstats')
            tagargs=['-tags='+','.join(tags)] if tags else []
            row=bylabel[group+'-build']; argv=row['command']
            assert argv==[summary['go_executable']['path'],'test','-c',*tagargs,*(['-race'] if group=='wago-race' else []),'-o',f'/home/runner/work/wago/wago/receipts-{mode}/{group}.test',package]
            assert row['cwd']=='/home/runner/work/wago/wago/source'
            build_commands[group]=argv
        assert summary['mode']==mode and summary['timer_control_ok']

        assert host['runner_arch']=='ARM64' and host['uname'][4]=='aarch64' and host['python_elf_machine']==183
        assert all(env[k]==v for k,v in {'GOOS':'linux','GOARCH':'arm64','GOHOSTOS':'linux','GOHOSTARCH':'arm64','GOVERSION':'go1.27.1','CGO_ENABLED':'1'}.items())
        integrity={}
        for label,pin in [('source',SOURCE),('harness',HARNESS)]:
            before=obj(label+'-before-manifest.json');after=obj(label+'-after-manifest.json')
            assert before==after==pinned_manifests[pin] and summary[label+'_unchanged']
            assert read(label+'-head.log').decode().strip()==pin
            tree=git('ls-tree','-rz',pin)
            for phase in ['before','after']:
                assert read(label+'-'+phase+'-tree.log')==tree
                assert not read(label+'-'+phase+'-status.log').strip()
                assert not read(label+'-'+phase+'-diff.log').strip()
            integrity[label]={'before_after_equal':True,'git_tree_matches_pin':True,'every_blob_sha256_matches_pin':True,'tracked_blob_count':len(before)}
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
            group=gate.removesuffix('-'+gate.split('-')[-1])
            binary=f'/home/runner/work/wago/wago/receipts-{mode}/{group}.test'
            regex='^('+'|'.join(expected)+')$'
            assert bylabel[gate]['command']==[summary['go_executable']['path'],'tool','test2json','-p',suite['package'],binary,'-test.v=test2json','-test.count=1','-test.timeout=3m','-test.run='+regex]
            assert bylabel[gate]['cwd']=='/home/runner/work/wago/wago/source/'+suite['package'].removeprefix('./')
            assert bylabel[gate+'-list']['command']==[binary,'-test.list='+regex]
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
            assert not skips and starts==passes and all(n==1 for n in passes.values()) and census['ok']
            gates[gate]={'expected':len(expected),'started':len(topstart),'passed':len(toppass),'fail':len(fails),'skip':len(skips),'not_run':sorted(set(expected)-set(topstart)),'exit':0,'census_ok':census['ok'],'all_pass_events':len(passes),'skipped_names':[e['Test'] for e in skips]}
            outcomes[gate]={k:'PASS' for k in passes if k.startswith(('TestRRRecheck','TestRRRetirement','TestSharedEntryGuardCancellationAndGC'))}
        assert len(gates)==6
        assert summary['census']['backend-backend']['all_test_passes']['TestRegisterCallOverlappingArgumentSlotARM64/direct']==1
        assert summary['census']['backend-backend']['all_test_passes']['TestRegisterCallOverlappingArgumentSlotARM64/indirect']==1
        assert sum(g['passed'] for g in gates.values())==192
        assert summary['go_executable']['sha256']=='1675694ef690db0f18fbe7046a886170904bede1d9db6ec96ae27945c1705c64'
        assert summary['wabt_executable']['sha256']=='26e6fe02eeed144d0c91d8fcdf257d994b33d7e6885aa93e92a54d45027d5cee'
        assert summary['go_executable']['elf_machine']==summary['wabt_executable']['elf_machine']==183
        provenance=dict(line.split('=',1) for line in read('wabt-provenance.txt').decode().splitlines())
        assert provenance['version']=='1.0.41' and provenance['platform']=='linux-arm64'
        assert provenance['binary_sha256']==summary['wabt_executable']['sha256']
        assert provenance['bootstrap_sha256']==plan['bootstrap_sha256']
        assert provenance['archive_sha256']=='5e35416ee8725dc7cc0572e4392a8117cbf008b0e34c0db65c75506b0299cdbf'

        timer_rows=[dict(item.split('=',1) for item in line.split() if '=' in item) for line in read('timer-control.log').decode().splitlines()]
        assert {(r['notify'],r['signo'],r['errno']) for r in timer_rows}=={(str(n),str(s),'0') for n in [1,0,4] for s in [35,64]} and len(timer_rows)==6
        assert all(r['arch']=='arm64' and r['clock']=='1' and r['size']=='64' for r in timer_rows)
        # Human-readable receipts are exact archive bytes. Full manifests/binaries remain in raw ZIP.
        for n in z.namelist():
            if n.startswith(prefix) and (n.endswith('.json') or n.endswith('.log') or n.endswith('.txt')) and '-manifest.' not in n and '-tree.' not in n:
                save(mode+'/'+n.removeprefix(prefix),z.read(n))
        audit['modes'][mode]={'artifact_sha256':sha(data),'artifact_bytes':len(data),'setup':'PASS','native_execution':'RUN','qualified':True,'driver_exit':0,'integrity':integrity,'gates':gates,'original_probe_outcomes':outcomes,'probe_sha256':probes,'timer_control_sha256':sha(timer),'timer_control_rows':timer_rows,'binaries':binaries,'go_executable':summary['go_executable'],'wabt_executable':summary['wabt_executable'],'command_count':len(commands),'nonzero_or_pending_commands':[],'phase':'all receipt gates pass','build_commands':build_commands}
preserved=json.loads((P/'preservation-before.json').read_text())
assert sha((P/'dispatch.json').read_bytes())==preserved['dispatch']
for path,digest in preserved['prior'].items(): assert sha(Path(path).read_bytes())==digest
assert git('diff','--cached','--raw').decode()==preserved['index']
audit['prior_failed_evidence_unchanged']=True
audit['dispatch_unchanged']=True
js('audit.json',audit)
print(json.dumps({m:{'gates':v['gates'],'integrity':v['integrity']} for m,v in audit['modes'].items()},indent=2))
