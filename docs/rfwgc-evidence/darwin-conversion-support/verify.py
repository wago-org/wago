#!/usr/bin/env python3
"""Offline evidence validation; no builds, benchmarks, execution or network."""
import hashlib,json,pathlib,subprocess
root=pathlib.Path(__file__).resolve().parent
for line in (root/'SHA256SUMS').read_text().splitlines():
 digest,name=line.split('  ',1)
 assert hashlib.sha256((root/name).read_bytes()).hexdigest()==digest,name
manifest=json.loads((root/'qualified-3242c18a1/source-manifest.json').read_text())
tree={line.split('\t')[1]:line.split()[2] for line in subprocess.check_output(['git','ls-tree','-r','3242c18a1'],text=True).splitlines()}
for row in manifest:
 assert tree[row['path']]==row['git_blob'],row['path']
 data=subprocess.check_output(['git','cat-file','blob',row['git_blob']])
 assert hashlib.sha256(data).hexdigest()==row['sha256'],row['path']
for directory,count in [('qualified-3242c18a1-tools',15),('targets-3242c18a1',15),('fallback-7e41fafbb',3)]:
 results=json.loads((root/directory/'results.json').read_text())
 assert len(results)==count,(directory,len(results))
 assert all(r['exit']==0 for r in results),directory
for row in json.loads((root/'targets-3242c18a1/event-census.json').read_text()):
 if '-qemu.log' in row['log']:
  assert row['counts']['PASS']>0 and row['counts']['FAIL']==row['counts']['SKIP']==0,row
for row in json.loads((root/'portable-body-audit.json').read_text()):
 before=subprocess.check_output(['git','show','3f6161883^:'+row['old']]).split(b'\n',1)[1]
 after=subprocess.check_output(['git','show','3f6161883:'+row['new']]).split(b'\n',1)[1]
 assert before==after,row
print('PASS: evidence hashes, source manifest, 33 final command receipts, nonempty zero-skip QEMU suites, unchanged portable bodies')
print('Native Darwin execution, native TinyGo and final combined qualification remain outside this local evidence.')
