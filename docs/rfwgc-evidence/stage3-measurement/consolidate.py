#!/usr/bin/env python3
"""Post-acquisition report, not a benchmark. Bootstrap matched BLOCKS, keep controls."""
import collections,json,math,pathlib,random,statistics,sys
root=pathlib.Path(sys.argv[1])
plan=json.loads((root/"plan.json").read_text())
blocks=collections.defaultdict(dict);rejected=[]
for line in (root/"telemetry.jsonl").read_text().splitlines():
 row=json.loads(line)
 if row["event"]!="block":continue
 key="/".join(row["cell"])
 if not row["accepted"]:rejected.append(row);continue
 if row["round"] in blocks[key]:raise ValueError("duplicate accepted round")
 assert all(.9<=v<=1.1 for v in row["controls"].values())
 blocks[key][row["round"]]=row
result=[]
for _,family,name in plan["cells"]:
 key=family+"/"+name;rows=blocks[key]
 if len(rows)!=plan["rounds"]:raise ValueError("incomplete cell "+key)
 logs=[]
 for r in rows.values():
  a,b=r["values"]["A"],r["values"]["B"]
  logs.append(statistics.mean([math.log(b[i]/a[i]) for i in range(2)]))
 rng=random.Random(20261101)
 samples=sorted(math.exp(statistics.mean(rng.choices(logs,k=len(logs)))) for _ in range(10000))
 result.append({"cell":key,"blocks":len(logs),"pairs":2*len(logs),
 "median_block_ratio":math.exp(statistics.median(logs)),
 "block_bootstrap_95_percent":[samples[249],samples[9749]],
 "controls":[r["controls"] for r in rows.values()],
 "provisional":"adverse" if samples[249]>1 else "favorable" if samples[9749]<1 else "unresolved"})
print(json.dumps({"cells":result,"rejected_blocks":rejected,
 "overall":"NO AUTOMATIC VERDICT: inspect thermal/load telemetry, correctness and coverage; any adverse cell blocks GAIN",
 "scope":"ratios B/A; six blocks; seed 20261101; 10000 resamples; no multiple-comparison adjustment"},indent=2))
