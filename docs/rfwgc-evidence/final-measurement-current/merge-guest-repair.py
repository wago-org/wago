#!/usr/bin/env python3
"""Bind corrected guest adapters without deleting earlier failure receipts."""
import pathlib,json
H=pathlib.Path(__file__).resolve().parent
old=H/'qualification/builds.json';repaired=H/'qualification-guest-repair/builds.json'
b=json.loads(old.read_text());new=json.loads(repaired.read_text());saved=H/'qualification/builds-before-guest-repair.json';assert not saved.exists();saved.write_bytes(old.read_bytes())
for key,values in new.items():
 assert key.endswith('/guest') and key.split('/')[1] in ['baseline','port']
 assert set(values)=={'ordinary','wagodebug','wago_regalloccheck'}
 assert all(v['build']['exit']==0 for v in values.values())
 b[key]=values
old.write_text(json.dumps(b,indent=2)+'\n')
r=H/'qualification/receipts.json';prior=json.loads(r.read_text());extra=json.loads((H/'qualification-guest-repair/receipts.json').read_text());r.write_text(json.dumps(prior+extra,indent=2)+'\n')
(H/'adapter-repair.json').write_text(json.dumps({'status':'CORRECTED_HARNESS_BUILD_COLLISION','cause':'041f source already includes TestDesignReviewExternStorageImportsPrivateGCProducer and TestDesignReviewExternStorageProducerCrossRuntimeImport; historical overlay names collided','change':'Rename both copied probe entrypoints TestCurrentPreparationDesignReview... identically on baseline and candidate; test bodies unchanged; rebuild/check both guest adapters in all three modes','preserved_failed_builds':[v['log'] for v in prior if v['exit']!=0],'replacement_keys':sorted(new),'production_modified':False,'runtime_finding':False},indent=2)+'\n')
print('Merged corrected guest bindings; retained all original command receipts')
