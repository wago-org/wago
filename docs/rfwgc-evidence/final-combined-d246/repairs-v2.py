import run as q
import json
for mode in ['ordinary','wagodebug','wago_regalloccheck']:
 if not (q.E/('repair-census-'+mode)/'result.json').exists() and q.test('repair-census-'+mode,['./src/wago'],mode,['-failfast','-run','^Test(Design|R9(H2|Ownership)|GCExternConversion|HostCallNeedsGCSuspension)']):raise SystemExit(1)
 r=json.loads((q.E/('repair-census-'+mode)/'result.json').read_text())
 required=['TestDesignRepairPublicGlobalTokenClassification','TestDesignReviewExternStorageProducerCrossRuntimeImport','TestDesignReviewExternStorageImportsPrivateGCProducer','TestDesignRepairWaitNotifier','TestDesignRepairConversionAdmissionLifecycle','TestDesignRepairScalarRelayOrdinaryStorageWithConversion','TestDesignRepairConcurrentPublicationRetirement','TestDesignRepairPublicTableOrdinary']
 passed={x.get('Test') for x in r['events'] if x['Action']=='pass' and x.get('Test')}
 assert set(required)<=passed, sorted(set(required)-passed)
 (q.E/('repair-census-'+mode)/'census.json').write_text(json.dumps(dict(required=required,passed=sorted(passed)),indent=2))
