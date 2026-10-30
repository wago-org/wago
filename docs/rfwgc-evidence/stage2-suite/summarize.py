#!/usr/bin/env python3
"""Summarize retained raw Go output, preserving exact failing test names."""
import json
from pathlib import Path
import re
root=Path(__file__).resolve().parent
summary={}
for name in ['ordinary','regalloccheck','ordinary-available-runtime','regalloccheck-available-runtime','cli-runtime']:
    path=root/(name+'.log')
    if not path.exists(): continue
    lines=path.read_text().splitlines()
    passed=[s.split()[1] for s in lines if s.startswith('ok\t') or s.startswith('ok  ')]
    failed=[s.split()[1] for s in lines if re.match(r'^FAIL\s+\S',s)]
    no_tests=[s.split()[1] for s in lines if s.startswith('?') and '[no test files]' in s]
    failures=[s.strip() for s in lines if re.match(r'^\s*--- FAIL:',s)]
    summary[name]={'package_counts':{'pass':len(passed),'fail':len(failed),'no_tests':len(no_tests)},'failed_packages':failed,'failing_test_lines':failures}
(root/'summary.json').write_text(json.dumps(summary,indent=2)+'\n')
print(json.dumps(summary,indent=2))
