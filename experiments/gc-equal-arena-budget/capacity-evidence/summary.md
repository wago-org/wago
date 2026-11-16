# Old-live-set capacity qualification

Source `2a7b976e`, production unchanged from the equal-budget run. Normal and
race invocations both passed all 27 configurations, with identical structured
receipts. No timing or V8 comparison was performed. The final shell log-display
command errored after both successful tests; it did not affect either test run.

| Young / each-old MiB | Entire target graph fits | Expected capacity OOM | Exact maximum tested live bytes |
|---|---:|---:|---:|
| 2 / 8 | 8 | 1 | 8,388,608 |
| 4 / 7 | 5 | 4 | 7,340,032 |
| 6 / 6 | 2 | 7 | 6,291,456 |

For each split, an exact-capacity graph succeeds and the corresponding graph
16 bytes larger fails both Minor and Full with ErrOOM. Larger requested graphs
stop at their first capacity-crossing child; receipts distinguish requested live
bytes from that attempted amount. These are predictable per-old-space limits,
not accidental early OOM or observed reference corruption.

All failed collections preserved roots, old cursor and checked payload IDs.
Dropping the newest edge enabled moving Full recovery. All surviving graphs
then passed two additional moving Full collections. Dropping all roots reclaimed
old storage, and fresh allocation plus Minor succeeded in every configuration.
Old-to-young reference writes were exercised through the persistent root array.

Each heap retained the same FixedBytes value of 28,463,151. Equal total reserved
memory therefore does not preserve maximum retainable live-set size: nursery
expansion from 2 to 6 MiB reduces maximum old live capacity by 2 MiB. This is an
explicit policy tradeoff, not a free performance improvement. Core qualification
does not establish the equivalent full runtime/guest workload behavior or justify
a universal default change.
