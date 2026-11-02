"""Require repaired regression bodies, full race scope, and three-mode execution."""
import pathlib,json,gzip,re
E=pathlib.Path(__file__).resolve().parent
modes=['ordinary','wagodebug','wago_regalloccheck'];rows=[]
arm=['TestDesignRepairPublicGlobalTokenClassification','TestDesignReviewExternStorageProducerCrossRuntimeImport','TestDesignReviewExternStorageImportsPrivateGCProducer','TestDesignRepairWaitNotifier','TestDesignRepairConversionAdmissionLifecycle','TestDesignRepairScalarRelayOrdinaryStorageWithConversion','TestDesignRepairConcurrentPublicationRetirement','TestDesignRepairPublicTableOrdinary']
rv=['TestDesignRepairPublicGlobalTokenClassification','TestDesignRepairPublicTableOrdinary','TestDesignRepairUnsupportedConvertedPublicToken']
tg=rv+['TestDesignRepairUnsupportedConvertedPublication','TestDesignRepairUnsupportedBulkAtomicity']
def verify(label,want,passed,skipped):
 missing=sorted(set(want)-set(passed));rows.append(dict(suite=label,required=want,passed=sorted(passed),missing=missing,skips=skipped));assert not missing,(label,missing)
for m in modes:
 d=E/('repair-census-'+m);r=json.loads((d/'result.json').read_text());assert r['exit']==0
 verify(d.name,arm,[x['Test'] for x in r['events'] if x['Action']=='pass' and x.get('Test')],[x for x in r['events'] if x['Action']=='skip'])
 d=E/('race-'+m);c=json.loads((d/'command.json').read_text());r=json.loads((d/'result.json').read_text());assert r['exit']==0 and '-race' in c['argv'] and not any(x.startswith('-run') for x in c['argv'])
 assert set(['./src/core/runtime/gc/...','./src/core/runtime','./src/wago','./tests/integration/...'])<=set(c['argv'])
 rows.append(dict(suite=d.name,full_race_scope_verified=True,argv=c['argv']))
rs=json.loads((E/'qemu-repairs/results.json').read_text())
for arch,want in [('arm64',arm),('riscv64',rv)]:
 for m in modes:
  name=arch+'-'+m+'-guest';r=next(r for r in rs if r['label']==name);assert r['exit']==0
  passed=[x.group(1) for l in r['pass'] if (x:=re.match(r'^--- PASS: ([^ /]+)',l))]
  verify('qemu-repairs/'+name,want+['TestPlatformExecutionArchitecture'],passed,r['skip'])
for m in modes:
 d=E/('tinygo-repair-'+m);r=json.loads((d/'result.json').read_text());assert r['exit']==0
 text=gzip.open(d/'raw.log.gz','rt').read();passed=re.findall(r'^--- PASS: ([^ /]+)',text,re.M);skips=re.findall(r'^--- SKIP:.*$',text,re.M)
 verify(d.name,tg,passed,skips)
(E/'repair-final-census.json').write_text(json.dumps(rows,indent=2)+'\n')
print('repair/race census',len(rows),'verified cells')
