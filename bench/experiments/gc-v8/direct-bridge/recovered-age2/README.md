# Fresh age2 collector comparison preparation

Pending final qualified canonical source containing SSE and age2 together. One identical binary: wago_gc_compact_range_sse2,wago_gc_compact_refstruct,wago_gc_compact_age2; runtime ExperimentalCompactAge2 false/true. No weighted checksum/shared/stride/cohort/shadow tags. Both compile identical artifacts under the same compiler policy; allocation elision OFF.

Equal TOTAL young budget2 MiB and two8 MiB old semispaces. Promote-all has active2 MiB; age2 has active1 MiB +inactive1 MiB, survivor cap512 KiB. Oversized allocation threshold therefore changes from2 MiB to1 MiB. This is an equal-budget policy comparison, not equal active nursery capacity. Record counters, RSS, Goheap and geometry; do not hide increased minor frequency or changed oversized classification.

All six exact artifacts on recovered Intel8573C host: separate128-warmup actualGC audits, bounded3 warmups/5 timedcalls/3 rotatingprocesses. Preserve controls/outliers. After qualified baseline validation, full six-case V8 gate uses original100+9x32/3rounds. Historical AMD timings are not same-machine baselines. No builds until explicitrelease.
