"""Summarize CSV samples; keep single-entry timings separate from batched calls."""
import csv
import statistics
import sys
from collections import defaultdict
from pathlib import Path

print("file,engine,callback,calls,host_ns_per_call,min,max,control_ns_per_iteration,net_ns_per_call")
for filename in sys.argv[1:]:
    groups = defaultdict(list)
    with open(filename) as source:
        for engine, callback, count, host, sample, ns in csv.reader(source):
            groups[engine, callback, int(count), int(host)].append(float(ns))
    for (engine, callback, count, host), values in sorted(groups.items()):
        if host != 1:
            continue
        controls = groups[engine, callback, count, 0]
        control = statistics.median(controls) if controls else None
        median = statistics.median(values)
        control_text = f"{control:.4f}" if control is not None else ""
        net_text = f"{median-control:.4f}" if control is not None else ""
        print(f"{Path(filename).name},{engine},{callback},{count},{median:.4f},"
              f"{min(values):.4f},{max(values):.4f},{control_text},{net_text}")
