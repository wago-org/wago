from pathlib import Path
import datetime, fcntl, hashlib, json, os, re, subprocess
R = Path(os.environ['WAGO_PROOF_ROOT']).resolve()
S = R / 'owned-forest-fixture'
P = S / 'src/core/compiler/backend/railshot/amd64'
SRC_NAME = 'owned_forest_compact_layout_independent_test.go'
D = Path(__file__).resolve().parent
H = R / 'owned-forest-transition-proof/harness'
G = R / 'toolchains/go1.27.1/bin/go'
graph = D / 'go.mod'
grsum = D / 'go.sum'
HEAD = '4ad04af0cfb9da58a1a08b513a8f659001deafd4'
PKG = 'github.com/wago-org/wago/src/core/compiler/backend/railshot/amd64'
hs = lambda p: hashlib.sha256(p.read_bytes()).hexdigest()
def state():
    return dict(head=subprocess.check_output(['git','-C',S,'rev-parse','HEAD'],text=True).strip(),
        tree=subprocess.check_output(['git','-C',S,'rev-parse','HEAD^{tree}'],text=True).strip(),
        status=subprocess.check_output(['git','-C',S,'status','--porcelain'],text=True).strip())
sourceBefore = state()
assert sourceBefore['head'] == HEAD and not sourceBefore['status'], sourceBefore
lock=(D/'run.lock').open('a')
fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB)
j=(D/'journal.jsonl').open('x')
def event(kind,**kw):
    j.write(json.dumps(dict(stage=kind,utc=datetime.datetime.now(datetime.timezone.utc).isoformat(),**kw))+'\n')
    j.flush();os.fsync(j.fileno())
# Keep the vetted dependency versions but the true Wago main-module identity for
# Go's internal-package checks on an explicit named-file package.
g=(H/'go.mod').read_text()
g=g.replace('module github.com/wasmbench/wasmbench/adapters/wago','module github.com/wago-org/wago')
g=re.sub(r'(?m)^\s*github\.com/wago-org/wago v[^\n]*\n','\n',g)
g=re.sub(r'(?m)^replace github\.com/wago-org/wago => [^\n]*\n','',g)
graph.write_text(g)
grsum.write_bytes((H/'go.sum').read_bytes())
env=os.environ.copy()
flags=json.loads((R/'stream-pmd-proof/page-all/arms.json').read_text())[0]['env']
env.update(flags)
env.update(GOWORK='off',GOTOOLCHAIN='local',GOFLAGS='',GOMAXPROCS='1',GOOS='linux',GOARCH='amd64',CGO_ENABLED='0',GOPROXY='off',GOSUMDB='off',GOCACHE=str(R/'go-cache'),GOMODCACHE=str(R/'go-modcache'),TMPDIR=str(R/'tmp'),WAGO_MAPPED_OPERAND_ARENA='1',WAGO_DEBUG_PANIC='0')
for k in ('WAGO_BOUNDS','WAGO_EXPLAIN','WAGO_LAYOUT_PROBE_ARM'):
    env.pop(k,None)
json.dump(dict(source=sourceBefore,hashes={str(p):hs(p) for p in [D/'run.py',P/SRC_NAME,graph,grsum,H/'go.mod',H/'go.sum']},flags=flags,graph_main='github.com/wago-org/wago',tests=['TestOwnedForestCompactLayoutOrdinary','TestOwnedForestFlushPlanTupleOrdinary','TestOwnedForestFlushPlanReentrantOrdinary']), (D/'receipt.json').open('x'),indent=2)
event('start',source=sourceBefore)
binaries={}
try:
    for variant,tags in [('wide','wago_owned_forest_layout_probe'),('compact','wago_owned_forest_layout_probe,wago_owned_forest_compact')]:
        base=['taskset','-c','2',str(G)]
        cmd=base+['list','-mod=readonly',f'-modfile={graph}',f'-tags={tags}','-json',PKG]
        with (D/f'{variant}-package.json').open('x') as o, (D/f'{variant}-list.log').open('x') as e:
            p=subprocess.run(cmd,cwd=P,env=env,stdout=o,stderr=e,pass_fds=(lock.fileno(),))
        event('inventory',variant=variant,command=cmd,returncode=p.returncode)
        assert p.returncode==0
        obj=json.loads((D/f'{variant}-package.json').read_text())
        assert Path(obj['Dir'])==P and obj['ImportPath']==PKG, obj
        files=obj['GoFiles']
        assert SRC_NAME in obj['TestGoFiles'] and SRC_NAME not in files
        assert not any(obj.get(k) for k in ('CgoFiles','CFiles','CXXFiles','MFiles','HFiles','FFiles','SFiles','SwigFiles','SwigCXXFiles','SysoFiles','EmbedFiles'))
        assert all(Path(n).name==n and n.endswith('.go') and not n.endswith('_test.go') for n in files)
        inv=dict(files={n:hs(P/n) for n in files+[SRC_NAME]})
        with (D/f'{variant}-source-files.json').open('x') as f: json.dump(inv,f,indent=2)
        binary=D/f'{variant}.test'
        cmd=base+['test','-c','-o',str(binary),'-p','1','-pgo=off','-mod=readonly',f'-modfile={graph}',f'-tags={tags}',*files,SRC_NAME]
        with (D/f'{variant}-build.log').open('x') as f:
            p=subprocess.run(cmd,cwd=P,env=env,stdout=f,stderr=subprocess.STDOUT,pass_fds=(lock.fileno(),))
        event('build',variant=variant,command=cmd,returncode=p.returncode)
        assert p.returncode==0
        binaries[variant]=binary
        event('binary',variant=variant,sha256=hs(binary))
    for arm,variant in [('wide-list','wide'),('wide-forest','wide'),('compact-forest','compact')]:
        suffix='|TestOwnedForestFlushPlanTupleOrdinary|TestOwnedForestFlushPlanReentrantOrdinary' if arm=='compact-forest' else ''
        cmd=['taskset','-c','2',str(binaries[variant]),'-test.v','-test.count=1','-test.timeout=60s','-test.run',f'^(TestOwnedForestCompactLayoutOrdinary{suffix})$']
        with (D/f'{arm}-focused.log').open('x') as f:
            p=subprocess.run(cmd,cwd=D,env=dict(env,WAGO_LAYOUT_PROBE_ARM=arm),stdout=f,stderr=subprocess.STDOUT,pass_fds=(lock.fileno(),))
        event('execute',arm=arm,command=cmd,returncode=p.returncode)
        assert p.returncode==0
    # Both comparison directions are strict. Counters/outputs are not normalized away.
    # Runtime oracles already executed on the image whose digests appear in each row.
    comparisons=[]
    def rows(arm,kind):
        return [json.loads(l.split(f'LAYOUT_{kind} ',1)[1]) for l in (D/f'{arm}-focused.log').read_text().splitlines() if f'LAYOUT_{kind} ' in l]
    sets={}
    for arm in ('wide-list','wide-forest','compact-forest'):
        cs,ex,mp=rows(arm,'COMPILE'),rows(arm,'EXEC'),rows(arm,'MAPPING')
        key=lambda v:(v['case'],v['intr'])
        sets[arm]={key(v):v for v in cs}
        assert len(cs)==len(ex)==len(sets[arm])==12 and sum(v['calls'] for v in ex)==38
        assert {key(v) for v in ex}==set(sets[arm]) and all(v['arm']==arm for v in cs+ex+mp)
        assert not mp
    for other in ('wide-list','compact-forest'):
        bridge=sets['wide-forest']
        assert set(bridge)==set(sets[other])
        for k,b in sorted(bridge.items()):
            a=sets[other][k]
            comparisons.append(dict(other=other,case=k[0],intr=k[1],body=a['bodyHashes']==b['bodyHashes'],native=a['nativeSHA']==b['nativeSHA'],encoder=a['encoderSHA']==b['encoderSHA'],nativeHashes=[a['nativeSHA'],b['nativeSHA']],encoderHashes=[a['encoderSHA'],b['encoderSHA']],bytes=[a['codeBytes'],b['codeBytes']]))
    with (D/'comparisons.json').open('x') as f: json.dump(comparisons,f,indent=2)
    assert len(comparisons)==24 and all(c['body'] and c['native'] and c['encoder'] for c in comparisons)
    prev=R/'owned-forest-sparse-hint-proof/summary.json'
    assert hs(prev)=='3788ed1ba7a67713e6a3d2f210f5a079e973918e36fb953e0e68fca126e65bb7'
    historic=json.loads(prev.read_text())['compile_rows']
    for arm,prior in historic.items():
        for b in prior:
            n=sets[arm][b['case'],b['intr']]
            assert {k:v for k,v in n.items() if k!='flushPlan'}==b,(arm,b['case'],b['intr'])
    counted=[]
    for k,w in sets['wide-forest'].items():
        planBefore=w['flushPlan'];planAfter=sets['compact-forest'][k]['flushPlan']
        for field in ('CanonicalBitExits','CanonicalLayoutExits','Plans','WideRoutes','NarrowRoutes'):
            assert planBefore[field]==planAfter[field],(k,field,planBefore,planAfter)
        assert planBefore['FusedPlans']==0 and planAfter['FusedPlans']==planAfter['Plans']
        assert planBefore['RootVisits']==planBefore['RootTypeReads']==2*planAfter['RootVisits']==2*planAfter['RootTypeReads']
        counted.append(dict(case=k[0],intr=k[1],wide_forest=planBefore,compact_forest=planAfter))
    assert any(x['compact_forest']['FusedPlans'] for x in counted)
    direct=rows('compact-forest','FLUSH_EMIT')
    assert len(direct)==6 and len({(x['lane'],x['fused']) for x in direct})==6 and all((x['v128SpillsDuringFlush']>0 and x['gpSpillsDuringFlush']==0) if x['lane']=='v128-wrapper-pressure-wide' else (x['gpSpillsDuringFlush']>0 and x['v128SpillsDuringFlush']==0) for x in direct)
    for lane in {x['lane'] for x in direct}:
        a,b=[x for x in direct if x['lane']==lane]
        assert not a['fused'] and b['fused']
        for field in ('slots','nativeBytes','gpSpillsDuringFlush','v128SpillsDuringFlush','typeScratchCap','gcScratchCap','narrowTypeScratchCap'):
            assert a[field]==b[field],(lane,field)
    with (D/'plan-work.json').open('x') as f:json.dump(dict(ordinary=counted,direct=direct),f,indent=2)
    event('exit',returncode=0,source=state())
except BaseException:
    event('exit',returncode=1,source=state())
    raise
finally:
    assert state()==sourceBefore
