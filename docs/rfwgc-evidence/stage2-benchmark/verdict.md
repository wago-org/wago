## Verdict

Guest publication reduction is measured: qualifying Stage2 On makes 1 materialized
object / published handle in 1024 fresh calls versus baseline Off/On and Stage2 Off
1024, with 1023 handleless charges. All four still charge 1024 allocations, consume
24576 layout bytes, and show 24576 bytes observed nursery advance / retained end.
Calculated throughput rounded requests remain32768B; this is not measured physical
reservation traffic. Nonqualifying remains1024 materializations/charges; no-GC
remains0. Host runtime allocations remain0 B/op and0 allocs/op in all twelve cells.
This extends, and does not replace, the existing128-call128->1 publication proof.

Qualifying runtime On is276.7ns/op versus baselineOn279.2ns/op (-0.90% latency,
+0.90% reciprocal throughput); ranges overlap275–285.9 versus276.4–286.8ns and CVs
1.48%/1.62%. Within Stage2, On versusOff287.8ns is-3.86% latency (+4.01% throughput).
Controls also move: Off runtime costs rise3.15–3.83% across revisions, nonqualifying
On rises3.98% and no-GCOn5.05%. This does not establish a robust net runtime speedup
against the quiet baseline; the qualified result is a modest local On/Off effect
plus clear guest-publication reduction. It is not a general application claim.

Compiler cost is substantial: qualifyingOn160469->352295.5ns (+119.54% versus
baselineOn); host147496->299472B/op and692->2463allocs/op. Stage2On versusOff is
+171.28%, versus newly measured baseline+24.99%. NonqualifyingOn rises8.16% across
revisions (host124982->125750B/op,309->341allocs/op); no-GCOn rises3.83% (host38064B,
57allocs unchanged). Off compiler changes are+1.15%,+1.35%,+1.54% qualifying,
nonqualifying,no-GC respectively, with unchanged host allocation counts. Prior
quiet Stage1 option costs+23.34/+4.22/+2.35% are context, not Stage2 measurements.
Keep the optimization opt-in; guest handle publication savings do not justify a
claim of free allocation removal or a clear end-to-end throughput win here.

The fresh matched dataset took18m19s including quiet waits:72 accepted pairs,
24 rejected pairs, six accepted500ms samples per cell. Accepted monitored maxima:
4.7873% external host and2.9703% SMT sibling; full raw telemetry retained. Initial
long-group and preparation attempts extend total wall time beyond that fresh
window. No statistical significance test or exclusive-machine isolation; CV and
min/max are descriptive. Tiny timing omitted; existing fallback tests are the
only Tiny evidence. No new sensitive-engine issue was fixed or investigated.
