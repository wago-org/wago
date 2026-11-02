#!/usr/bin/env python3
"""Summarize completed immutable correctness receipts without hiding skips."""
import hashlib,json,pathlib
root=pathlib.Path(__file__).resolve().parent
pin='9180f09f7590d029bc90df2fa20c0decc590505a'
summary={'source_pin':pin,'target_execution_missing':['darwin/arm64','windows/amd64','TinyGo'],'suites':{}}
for directory,expected in [('integrated-9180f09f7',18),('targets-9180f09f7',14),('nonhost-9180f09f7',6)]:
 d=root/directory;rows=json.loads((d/'results.json').read_text())
 assert len(rows)==expected,(directory,len(rows))
 for row in rows:
  log=d/(row['label']+'.log');lines=log.read_text().splitlines()
  row['top_level_passes']=sum(s.startswith('--- PASS:') for s in lines)
  row['skip_entries']=sum('--- SKIP:' in s for s in lines)
  row['log_sha256']=hashlib.sha256(log.read_bytes()).hexdigest()
 assert all(r['exit']==0 for r in rows),directory
 summary['suites'][directory]=rows
meta=json.loads((root/'integrated-9180f09f7/driver-final.json').read_text())
assert meta['all_pass'] and meta['driver_unchanged'] and not meta['export_changes']
assert json.loads((root/'targets-9180f09f7/driver-final.json').read_text())['driver_unchanged']
(root/'summary.json').write_text(json.dumps(summary,indent=2)+'\n')
for directory,rows in summary['suites'].items():
 print(directory)
 for r in rows:print(r['label'],r['exit'],r['top_level_passes'],r['skip_entries'])

required=['TestDesignReviewExternStorageProducerCrossRuntimeImport','TestDesignReviewExternStorageImportsPrivateGCProducer','TestDesignRepairWaitNotifier','TestDesignRepairConversionAdmissionLifecycle','TestDesignRepairScalarRelayOrdinaryStorageWithConversion','TestDesignRepairConcurrentPublicationRetirement','TestDesignRepairPublicTableOrdinary']
for mode in ['ordinary','wagodebug','wago_regalloccheck']:
 for lane in ['focused','race']:
  text=(root/'integrated-9180f09f7'/(mode+'-'+lane+'.log')).read_text()
  assert all('--- PASS: '+name+' (' in text for name in required)
  assert '--- SKIP:' not in text
 text=(root/'targets-9180f09f7'/('arm64-'+mode+'-guest.log')).read_text()
 assert all('--- PASS: '+name+' (' in text for name in required)
 text=(root/'targets-9180f09f7'/('riscv64-'+mode+'-guest.log')).read_text()
 assert '--- PASS: TestDesignRepairUnsupportedConvertedPublicToken (' in text
 assert '--- PASS: TestDesignRepairPublicTableOrdinary (' in text
for target in ['darwin','windows']:
 text=(root/'nonhost-9180f09f7'/(target+'-selected-files.txt')).read_text()
 assert 'design_repair_unsupported_test.go' in text and 'design_repair_unsupported_token_test.go' in text
print('Required regression census: PASS')
