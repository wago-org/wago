#!/usr/bin/env python3
import json, pathlib, re, sys
folder = pathlib.Path(__file__).parent / sys.argv[1]
rows = json.loads((folder/'results.json').read_text())
for row in rows:
 lines=(folder/(row['mode']+'-'+row['suite']+'.txt')).read_text().splitlines()
 row['top_level_passes']=sum(x.startswith('--- PASS:') for x in lines)
 row['failures']=[x.strip() for x in lines if '--- FAIL:' in x]
 row['skips']=[x.strip() for x in lines if '--- SKIP:' in x]
(folder/'summary.json').write_text(json.dumps(rows,indent=2)+'\n')
print('\n'.join(f"{r['mode']} {r['suite']}: exit {r['exit']}, {r['top_level_passes']} top-level passes, {len(r['skips'])} skips" for r in rows))
