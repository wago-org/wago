#!/usr/bin/env python3
"""Availability census only; missing inputs remain in the denominator."""
import hashlib,json,pathlib,subprocess
ROOT=pathlib.Path(__file__).resolve().parents[3]
def sha(p):return hashlib.sha256(p.read_bytes()).hexdigest() if p.is_file() else None
cat=ROOT/"corpus/catalog.json"
catalog=json.loads(cat.read_text());rows=[]
for e in catalog["benchmarks"]:
 p=ROOT/"corpus"/e["artifact"];actual=sha(p)
 command=e.get("command",{})
 inputs=[]
 for name,expected in command.get("inputs",{}).items():
  q=ROOT/"corpus"/command["preopen"]/name
  inputs.append({"path":str(q.relative_to(ROOT)),"expected":expected,"actual":sha(q),"matches":sha(q)==expected})
 rows.append({"id":e["id"],"artifact":e["artifact"],"expected":e["artifact_sha256"],"actual":actual,
              "matches":actual==e["artifact_sha256"],"inputs":inputs,"execution":"NOT RUN"})
print(json.dumps({"catalog_sha256":sha(cat),"entries":len(rows),
 "unique_declared_binaries":len({r["expected"] for r in rows}),
 "available_entries":sum(r["actual"] is not None for r in rows),
 "hash_matching_entries":sum(r["matches"] for r in rows),
 "declared_input_references":sum(len(r["inputs"]) for r in rows),
 "matching_input_references":sum(i["matches"] for r in rows for i in r["inputs"]),
 "rows":rows},indent=2))
