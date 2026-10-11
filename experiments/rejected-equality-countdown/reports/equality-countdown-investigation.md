# Modular equality countdown experiment

Apple M4 Max, arm64. Opt-in only (`equality-countdown` / `WAGO_ARM64_EXPERIMENT_EQUALITY_COUNTDOWN=1`). Implemented in the isolated worktree `/tmp/wago-counted-loop-20261010` from `c74d21c1`; production root is unchanged.

The bounded decoded scan proves a sole equality backedge `bound != (counter += positive_constant)` with no escaping branch, nested loop, call, interruptible poll, EH/custom instruction or tracked root. The counter is written only by the latch and the bound is invariant. Both have existing permanent integer register homes. The scanner uses fixed storage (64 locals, 256 instructions, 1024 bytes), never reads workload identities or benchmark inputs, and declines regional control leases and existing vector-loop joins.

If the body does not read the counter, use its home for the modular distance `bound-counter`; `SUBS/BNE` replaces the counter ADD, comparison and backedge. Restore the original final counter from the unchanged bound on fall-through. Otherwise, if the body does not read the bound, use the bound home for the distance, retain the counter ADD and restore the bound from the final counter on fall-through. This variant removes the ADD-to-CMP branch dependency while preserving instruction count. No additional register is reserved and every original post-loop local value remains exact.

For every i32 initial value and step, `remaining = bound-counter (mod 2^32)`. Thus `remaining-step != 0` is exactly the original updated-counter inequality, including wrap and nonterminating congruence classes. A zero initial distance still executes the do-while body; the cover does not shortcut it.

Native fixtures pass both bounds modes, compact and ordinary code, live and dead counters, internal forward branches, step 1/2/4095, signed/unsigned high values and wrapping starts, and both original local observations after the loop. A division trap with initial counter equal to bound proves first-iteration trap ordering. Extra backedges, both homes read by the body, zero step and interruptible loops decline. Full backend/catalog/encoder diagnostics and all 148 signal-mode exact corpus oracles pass.

The initial native inventory changes 62 application images and seven core images versus the corrected pre-regional baseline. Focused paired execution/compilation measurements are queued; no gain or acceptable tradeoff is claimed yet. Explicit-bounds full qualification remains pending a positive timing result.


Initial SUBS/BNE form: ADPCM execution +1.58% and compilation +2.74%; register allocation -.17%/+2.88%; global alignment -1.63%/+2.43%; unchanged polygon compilation +4.89%. ADPCM's body instructions and loop-entry offsets were identical off/on, so the regression was not explained by moving its hot body.

Mitigations: a cheap canonical `ne; br_if 0; end` rejection filter precedes every decoded proof, and a six-entry ring replaces moving six instruction records at every operation. The filter establishes no semantic proof; noncanonical zero-label encodings are simply not attempted. Use SUB/CBNZ for the latch. This removes the first ADPCM execution regression (+.16%) and polygon's unnecessary compilation cost (+.42% paired median). Kmer's first mitigated execution result was -4.69% with +2.27% compilation, but a later broad screen was variable, so confirmation is required.

The final CBNZ form passes all 148 exact corpora in both bounds modes plus full diagnostics and an older-operand-stack decline fixture. The affected-image screen covers all 62 changed applications with four alternating 100ms pairs for execution and compilation. Its median paired execution delta is -.11% and median compilation delta +2.21%; strongest preliminary execution results are RLE -8.60%, median image filter -6.99%, LCS -6.52%, and sort-merge join -4.73%. Raw scheduling outliers are retained. Bootstrap's apparent +38% reflects mixed 20/67/119us samples; it is not a proven regression. Strong candidates and roughly +2% regressions have a separate twelve-pair 300ms confirmation queued.

Two manifest-derived prepared profiler captures of Kmer pass their exact oracle, with code and Wasm source maps. They identify the histogram store as the main observed leaf site in both states; leaf observations are not CPU-time weights. These captures do not establish a timing gain.

A second compilation mitigation replaces read/write count arrays with four 64-bit once/twice masks. The recognizer needs only whether a local is read/written zero, once, or more than once; fixed masks retain that exact proof and eliminate array initialization and indexed count accesses. Native matrix and focused compiler costs are queued, with native-image identity verification required before retention.

Core compilation measurements completed for all seven changed core images. The first execution command failed because it included unsupported Blake3 vector and QOI memory-check contracts; no execution result is claimed from that failed command. A corrected job selects only the three supported simple exact-result contracts (YYJSON, fast_float, PCRE2). General vector/input-contract timing requires their own harness; all 148 correctness contracts have already passed.


For the structural dead-counter-only mitigation, a second canonical local.get byte pattern provides a cheap rejection before the decoded proof. Bytes from an immediate can only cause a conservative rejection; complete instruction decoding is still required for every admission. This can reduce the compiler cost of used-counter loops that the narrowed variant declines. A depth guard also declines before emission when the existing compact loop depth field cannot represent the control stack.
