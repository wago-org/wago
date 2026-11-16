# Separate compact compilation footprint

Three fresh processes, 256 Compile+Close iterations per artifact. No baseline compiler comparator is included in this gate.

| Case | Median us/op | Native bytes | Go B/op | Allocs/op |
|---|---:|---:|---:|---:|
| struct-allocation-access | 166.804 | 3183 | 68131 | 253 |
| array-allocation-access | 159.400 | 3439 | 71987 | 242 |
| array-fill | 168.036 | 3951 | 76299 | 246 |
| array-copy-checksum | 205.305 | 7343 | 80420 | 288 |
| array-loop-copy-checksum | 187.070 | 7343 | 80420 | 288 |
| tree-survival | 150.672 | 4015 | 69731 | 289 |

These are Go allocation totals per compile, not peak compiler RSS. Exact raw outputs remain in compile.json.