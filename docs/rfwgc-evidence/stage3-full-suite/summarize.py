#!/usr/bin/env python3
import collections,json,pathlib,gzip
root=pathlib.Path(__file__).resolve().parent
summary={}
for path in sorted(list(root.glob('*.jsonl')) + list(root.glob('*.jsonl.gz'))):
    counts=collections.Counter(); failures=[]; skips=[]; region=[]; malformed=[]
    data = gzip.open(path, 'rt').read() if path.suffix == '.gz' else path.read_text()
    for line in data.splitlines():
        try: e=json.loads(line)
        except ValueError:
            malformed.append(line); continue
        action=e.get('Action'); name=e.get('Test'); pkg=e.get('Package')
        if action not in ('pass','skip','fail'): continue
        level='package' if not name else ('subtest' if '/' in name else 'test')
        counts[level+'_'+action]+=1
        if action=='fail': failures.append([pkg,name])
        if action=='skip': skips.append([pkg,name])
        if name and ('TestEdenRegions' in name or 'TestGCEdenRegions' in name): region.append([pkg,name,action])
    summary[path.name.removesuffix('.gz').removesuffix('.jsonl')]={'counts':dict(counts),'failures':failures,'skips':skips,'region_tests':region,'non_json_lines':malformed}
(root/'summary.json').write_text(json.dumps(summary,indent=2)+'\n')
for name,s in summary.items(): print(name,s['counts'], 'failures=',len(s['failures']))
