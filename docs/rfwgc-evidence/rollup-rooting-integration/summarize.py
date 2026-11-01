#!/usr/bin/env python3
import hashlib,json,pathlib,re
out=pathlib.Path(__file__).resolve().parent
results={}
for file in sorted(out.glob('**/*.txt')):
 if file.name.endswith('exits.txt') or file.name in {'prerequisites-files.txt','cluster-files.txt','sibling-eh-audit.txt','sibling-none-audit.txt'}:continue
 text=file.read_text()
 if not ('=== RUN' in text or 'cross-' in file.name or file.name.startswith('prerequisites-')):continue
 results[str(file.relative_to(out))]={
  'passedTopLevel':len(re.findall(r'^--- PASS:',text,re.M)),
  'failedTopLevel':len(re.findall(r'^--- FAIL:',text,re.M)),
  'skippedTopLevel':re.findall(r'^--- SKIP: (\S+)',text,re.M),
  'failedLeaves':re.findall(r'^\s*--- FAIL: (\S+)',text,re.M),
 }
results['exitLogs']={str(p.relative_to(out)):p.read_text().splitlines() for p in sorted(out.glob('**/*exits.txt'))}
(out/'results.json').write_text(json.dumps(results,indent=2)+'\n')
files=sorted(p for p in out.rglob('*') if p.is_file() and p.name!='SHA256SUMS')
(out/'SHA256SUMS').write_text(''.join(hashlib.sha256(p.read_bytes()).hexdigest()+'  '+str(p.relative_to(out))+'\n' for p in files))
