import run as q
import json,gzip,hashlib
# Reuse only the successful exact-pin command retained before the census correction.
for mode in ['ordinary','wagodebug','wago_regalloccheck']:
 d=q.E/('repair-census-'+mode)
 if not d.exists():continue
 c=json.loads((d/'command.json').read_text());r=json.loads((d/'result.json').read_text())
 assert c['source']==r['source']==q.M['pin'] and r['exit']==0 and r['driver_unchanged']
 assert c['driver_sha256']==hashlib.sha256((q.E/'run.py').read_bytes()).hexdigest()
 assert r['raw_sha256']==hashlib.sha256(gzip.decompress((d/'raw.log.gz').read_bytes())).hexdigest()

for mode in ['ordinary','wagodebug','wago_regalloccheck']:
 if not (q.E/('repair-census-'+mode)/'result.json').exists() and q.test('repair-census-'+mode,['./src/wago'],mode,['-failfast','-run','^Test(Design|RR|SharedEntryGuardCancellationAndGC|R9(H2|Ownership)|GCExternConversion|HostCallNeedsGCSuspension)']):raise SystemExit(1)
 r=json.loads((q.E/('repair-census-'+mode)/'result.json').read_text())
 required=['TestDesignRepairPublicGlobalTokenClassification','TestDesignReviewExternStorageProducerCrossRuntimeImport','TestDesignReviewExternStorageImportsPrivateGCProducer','TestDesignRepairWaitNotifier','TestDesignRepairConversionAdmissionLifecycle','TestDesignRepairScalarRelayOrdinaryStorageWithConversion','TestDesignRepairConcurrentPublicationRetirement','TestDesignRepairPublicTableOrdinary']
 required += ['TestDesignInvocationSnapshotCancellation', 'TestDesignInvocationSnapshotLatePublisher', 'TestDesignInvocationSnapshotRetirement', 'TestDesignInvocationSnapshotAuthenticatedDispatch', 'TestDesignInvocationSnapshotCallbackTrap', 'TestDesignRecheckForeignRelayAfterLatePublisher', 'TestDesignRecheckStorageOnlyCallTopologyWriter', 'TestDesignRecheckCallbackChangesPublisherAuthority', 'TestDesignRecheckNestedSameRuntimeClear', 'TestDesignRecheckNestedPublish']
 required += ['TestRRRetirementLastPinAndRevival', 'TestRRRetirementCallbackAndExitWakeups', 'TestRRRetirementBorrowedRootLatePublisher', 'TestRRRetirementCanceledPartialLastPins', 'TestRRRetirementCallbackRefreshBehindReader', 'TestRRRecheckStorageOnlyReturnBehindDynamicReader', 'TestRRRecheckCanceledAdmissionBehindDynamicReader', 'TestRRRecheckSharedMemoryHandoff']
 passed={x.get('Test') for x in r['events'] if x['Action']=='pass' and x.get('Test')}
 assert set(required)<=passed, sorted(set(required)-passed)
 (q.E/('repair-census-'+mode)/'census.json').write_text(json.dumps(dict(required=required,passed=sorted(passed)),indent=2))
