# Fresh weighted-reduction comparison

Qualified source79add634, same-host Intel8573C. Baseline SSE+refstruct versus weighted_sum+refstruct; age2,shared,stride,cohort,shadow,metrics OFF. Two separate compiler binaries from identical source. Allocation elision OFF.

Both promote-all active2 MiB nursery and two8 MiB old spaces. Same six SHA-pinned artifacts, actual-GC128-warmup audits, bounded3warmups/5calls/3rounds, then full100warmups/9x32/3rotatingrounds with contemporaneous V8 --no-liftoff. Separate compile allocation/code-size measurements. Preserve every miss, control and outlier; no historical cross-host baseline. Parent79add634 qualification and architect explicit CPU release precede builds.
