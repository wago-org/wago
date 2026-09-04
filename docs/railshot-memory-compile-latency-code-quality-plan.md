# Railshot memory, compile-latency, and code-quality plan

I reviewed Wago’s current `main` at commit `b40f0305906928dd415ae677fdfba5f1a608f464`, including both Railshot backends, the byte-backed frontend, hint analysis, operand stack, register allocation, root analysis, parallel compilation, code-image ownership, the September 1 arena study, and the existing optimization research.

The central conclusion is:

> **Railshot does not need an IR, a more sophisticated whole-function allocator, or another pile of handwritten peepholes. Its next frontier is to make compiler state smaller, pointer-free, bounded, and shorter-lived—then combine instruction selection, allocation, and encoding within that bounded state.**

The work should happen in this order:

1. Compact retained function summaries.
2. Eliminate failed first compilation attempts.
3. Bound and discard per-worker high-water memory.
4. Make GC-root planning adaptive.
5. Replace the remaining pointer-rich operand node with compact IDs.
6. Add costed tree selection, rematerialization, and a tiny machine window.
7. Gradually retire the old representation behind whole-function eligibility.

This is evidence-backed and falsifiable. Nothing is “proven for Wago” until it passes the measurement gates below, but the first several stages are intentionally behavior-preserving and build directly on wins already measured in the repository.

---

## Current-main reconciliation — September 3, 2026

A follow-up audit at `779e5e65842359c1c7b169f1af299097853a71ad` found several additional general reductions and two places where current `main` is already ahead of this plan. The complete evidence and primary-source references are in [Railshot memory reduction follow-up](research/railshot-memory-reduction-2026-09-03.md).

- AMD64 already labels deferred trees with bounded register demand and uses it for semantics-safe evaluation ordering. Extend that analysis into pre-emission pressure prediction; do not build a second tree-labeling mechanism.
- Both backends already have a fixed 24-operation machine window for ABI shuffles. Generalize it only when counters identify missed machine-level combinations; do not add a parallel window.
- Compact pointer-rich `ctrlFrame` records before or alongside operand-node conversion. The implemented split now leaves ordinary frames at 104 bytes on both architectures; further control-state work should target measured sidecar high-water rather than restoring cold fields to the common record.
- Resolve module invariants once, narrow compiler-only indexes, flatten parallel metadata, and apply retention limits per scratch buffer.
- Retire default-off experiments and mature rollback switches that fail the normal qualification gates. Compiler mechanisms should replace old state, not accumulate beside it.

The implementation currently includes eighty general cuts from that audit:

1. Module hint scanning always retains exact touched-global records instead of a dense function-by-global matrix, and the fixed hint record drops from 200 to 152 bytes. On a synthetic 1,024-function/1,024-global shape with one touched global per function, this changed the ARM64 hint benchmark from approximately 5.47 MB and 0.64 ms per operation to 0.24 MB and 0.12 ms per operation. This is a targeted stress result, not a full-corpus claim.
2. Module-wide synchronous-host-call classification is computed once per module, and the bounded module-global pin list replaces a per-function `globals`-sized membership bitmap.
3. The existing opt-in statistics path now exposes a shared compile-resource ledger for hint headers and sidecars, function attempts, failed-attempt input/node/code bytes, and failed-attempt time. Timing is explicitly excluded from deterministic stats comparisons. Hint and retry byte/count fields remain deterministic; worker scratch retention fields intentionally report actual worker topology and scheduling and are excluded from serial/parallel artifact-stat comparisons.
4. The first control-stack cuts move EH-only state behind a lazy semantic sidecar, group scalar fields to remove alignment holes, and stop allocating all-false GC-root vectors. Together they reduce every ordinary `ctrlFrame` from 472 to 408 bytes on AMD64 and from 416 to 368 bytes on ARM64. On a generated 128-deep scalar-block benchmark, AMD64 allocations fell from 283 to 155 per compile and allocated bytes moved from roughly 229.7 KiB to 209.8 KiB; median latency was effectively flat across the before/after local screens. This is an adversarial shape result, not completion of the 32–48-byte control-frame target.
5. The failed, default-off `fcmp-fuse` experiment is retired on both architectures. Its screening showed no aggregate execution win, slightly worse compile time when enabled, unchanged compile allocation, and focused wins below the generated-code acceptance gate. Removing the deferred-node path, public option, environment controls, schema entry, and experiment-only test deletes a net 283 backend source lines while retaining eager float-comparison semantics and coverage. Like the other early changes, default native code remains byte-identical; the production binary-size change is negligible (-160 bytes AMD64, +104 bytes ARM64 in matched Linux builds), so this is claimed only as a source-complexity reduction.
6. The default-off AMD64 `call-next-use` experiment is also retired. Its bounded post-call bytecode rescan and alternate spill policy bought only 0.10% aggregate execution, with a worst focused result of 0.84%, while disabling it improved compile time by 0.25% and left compile allocation unchanged. The removal deletes a net 168 AMD64 backend lines and two per-function masks; matched Linux/AMD64 artifacts for recursive-call, many-function, and float fixtures remain byte-identical. Calls now have one general dirty-local spill policy rather than a dormant alternate path.
7. Immutable-table proofs are now retained once per module instead of copied into every function summary. This removes one slice header from AMD64 summaries and four module-wide scalar fields from ARM64 summaries, reducing `funcHints` from 152 to 128 bytes on both architectures. The `many_funcs` full-compile benchmark falls from 220,625 to 212,433 B/op (-3.7%) with allocation count unchanged; an eight-sample local screen showed no latency regression. Matched many-function, float, and indirect-dispatch artifacts remain byte-identical.
8. The reusable dense global-hint accumulator is now explicit scan scratch rather than a pointer field retained in every finished summary. This reduces `funcHints` from 128 to 120 bytes and removes one scanned pointer per function. The 1,024-function sparse-global stress benchmark falls from 216,280 to 208,088 B/op (-3.8%) with the same 24 allocations and unchanged steady-state latency; the smaller `many_funcs` allocation remains in the same Go size class. Matched many-function, global-heavy, and indirect-dispatch artifacts remain byte-identical.
9. Retained local-score, last-use, and sparse-global slice headers are replaced by checked 32-bit ranges into module-owned sidecars. Stack-local views reconstruct the slices only while scanning or compiling a function, and the compile boundary passes that larger view by pointer to avoid a Go ABI copy cliff. This reduces `funcHints` from 120 to 64 bytes on both architectures (-46.7%) and removes the temporary per-function sparse-range array. On ARM64, the 1,024-function sparse-global stress benchmark falls from 208,088 to 134,168 B/op (-35.5%) and from 24 to 21 allocations; `many_funcs` falls from 150,776 to 125,240 B/op (-16.9%) and from 42 to 39 allocations. Five-sample default-GC medians moved from 216.54 to 217.38 microseconds (+0.4%); with GC disabled they moved from 217.12 to 214.31 microseconds (-1.3%). Matched `many_funcs`, `globals`, `dispatch`, and `json-as` native-code hashes remain identical.
10. ARM64 module hint construction now stores its validated local count directly in the compact summary instead of retaining a duplicate host-width `[]int`. The 1,024-function stress benchmark falls again from 134,168 to 125,976 B/op and from 21 to 20 allocations; `many_funcs` falls from 125,240 to 122,552 B/op and from 39 to 38 allocations. This is the same general per-function metadata deduplication used by AMD64, not a corpus-selected path.
11. ARM64 whole-function pins now leave four unreserved registers for the widest ordinary lowering step: three simultaneously protected inputs or temporaries plus one result. The limit is derived from the target register file after module roles are removed; it never consults module identity, function identity, producer metadata, or corpus membership. This removes the two remaining retries in the complete benchmark corpus. On Ruby, attempts fall from 17,454 to 17,452, eliminating 4,299 re-read input bytes, 109,648 failed-attempt node bytes, and 4,060 discarded code bytes. Unlike the old all-or-nothing retry, the admitted first attempt retains 16 profitable pins in each affected function and shrinks the full native image by 2,592 bytes. Five one-shot full-compile medians moved from 608.95 to 610.21 milliseconds (+0.2%). The retry path remains temporarily as an instrumented correctness oracle until adversarial pressure and both architectures qualify at zero hits.
12. AMD64 now treats a pinned local as a cache rather than an irrevocable register reservation. At the exact allocation failure point, it can home one safe integer local to its canonical frame slot, lend the register to the current lowering step, and evict that temporary before restoring or rewriting the local. A compact pre-scan flag reserves additional transient registers only for deep variable-shift trees, whose count operand imposes an x86 fixed-register constraint; it is derived solely from decoded instruction shape and occupies an existing hint word. This removes all 16 observed retries across `regexmatch`, SQLite, and Ruby while leaving `tiny`, `json-as`, `utf-as`, and BLAKE native images byte-identical. With GC disabled under Rosetta, three-sample compile medians improved from 45.33 to 43.76 milliseconds on `regexmatch` and from 69.59 to 68.14 milliseconds on SQLite; allocated bytes and allocation counts also decreased. Ruby one-shot compile samples were 2–4% slower despite slightly lower allocation, so native AMD64 execution and compile-latency qualification remain open. Broad transient-register floors and call/scalar-pressure policies were tested and removed: they changed unaffected JSON code and regressed execution by roughly 3–22%. The retained mechanism has no producer, module, function, index, hash, or corpus-specific selector.
13. Parallel function results no longer duplicate the already-existing per-function relocation slice header or reserve an error interface for every successful function. Workers write relocations directly into their uniquely owned module slot, while one synchronized coordinator retains only the lowest-indexed error and preserves deterministic diagnostics. Direct-prepared and omitted booleans are folded into the existing layout byte. This reduces `funcResult` from 144 to 88 bytes on ARM64 (-38.9%) and from 152 to 104 bytes on AMD64 (-31.6%). On ARM64 `many_funcs` with four workers, 200-compile samples fell from roughly 332.6 KiB to 310.9 KiB per compile (-6.5%) with the same 105 allocations, identical 9,720-byte output, and effectively flat median latency. Three one-shot Ruby/four-worker samples showed about 1.3 MiB lower median allocation with the same 37,572,432-byte output; their timing was noisy and is not claimed as a latency win. This is a general ownership/lifetime cut: it depends only on each worker's exclusive function index and does not inspect function contents.
14. Exact GC-frame planning now uses its documented `nil` representation for `RootNone` functions. The existing semantic classifier first proves that a function has no call and no allocating GC instruction; such a frame cannot be observed at a collection safepoint, so the planner skips the 208-byte function plan, tracked-local vectors, and full liveness CFG. A caller in the same module still receives the ordinary exact plan. The per-function EH-root sidecar is also allocated only when exception analysis actually returns roots, rather than for every exact-GC module. On a synthetic 1,024-function module with 1,023 scalar leaves, one caller, and no EH roots, ARM64 planning fell from approximately 249.8 KiB and 1,029 allocations to 9.8 KiB and 5 allocations; five-sample median time moved from about 118.7 to 11.1 microseconds. These are exact semantic and ownership cuts selected only from decoded effects and EH analysis, not body size, identity, or corpus heuristics.
15. AMD64 parallel workers no longer all preallocate the deepest function's pointer-rich control-frame stack. Serial compilation retains the exact module maximum, while each parallel worker reserves at most eight frames and grows only if it actually receives deeper structured control. Both architectures now clear and release each worker's control backing before the parallel join, when it can no longer be reused, and the compile-resource ledger reports reserved, peak-envelope, retained, and discarded bytes. In a 64-function/four-worker stress module with one depth-40 outlier, initial control scratch falls from 66,912 to 13,056 bytes, the measured peak envelope is 36,720 bytes (45.1% below the former initial reservation), and retained join-time control scratch falls to zero. Five 100-compile samples reduced allocated bytes from roughly 254.8 to 242.2 KiB (-5.0%); median time moved from 167.7 to 165.8 microseconds. The ordinary `many_funcs`/p4 screen remained effectively flat. The eight-frame bound is a worker-memory ceiling with ordinary slice growth as the correctness fallback; it does not select code generation or inspect workload identity.
16. Parallel workers now also clear and release every pointer-rich operand-node chunk before the join. Function code, relocations, literals, and scalar feature flags are already independently owned at that point; no finalization step reads the operand stack. In the same four-worker control-outlier fixture, the ledger reports 114,688 bytes of initially reserved node scratch, the same peak envelope, zero retained bytes, and all 114,688 bytes discarded before final image assembly. This changes lifetime only: node sizing, growth, lowering, scheduling, and generated bytes are unchanged.
17. Worker exit now snapshots the node/control ledger into a pointer-free scalar record and clears the worker's `scratch` pointer before the join. This makes every other compiler-only buffer—local-state tables, temporary root slices, assembler recorders, branch maps, trap-site vectors, and feature-specific sidecars—unreachable as soon as its worker finishes. The join retains only code/literal arenas, compact function results, scalar feature flags, and the resource snapshot that it actually consumes. This is an ownership change with no opcode, function-shape, or corpus-dependent selection.
18. Inline planning now retains a dense 32-bit local-function index plus compact records and type sidecars only for callees that pass the existing semantic and target-cost admission. Previously, enabling inlining allocated a pointer-rich target record and local/result type capacity for every function as soon as any function contained a call. The retained target shrinks from 120 to 64 bytes on ARM64 and from 144 to 56 bytes on AMD64; a non-target now costs four pointer-free bytes. On the 301-function `many_funcs` fixture, which has three generally admitted leaf callees, full ARM64 compile allocation fell from approximately 122.6 to 103.4 KiB/op (-15.6%) and Linux/AMD64 from 125.2 to 95.9 KiB/op (-23.4%). Allocation count rises by two for the separated sidecars; an interleaved ARM64 binary screen was effectively flat to slightly faster (about 206.1 versus 208.0 microseconds median). Candidate selection, O(1) lookup, and generated code are unchanged; the representation never examines corpus identity.
19. Control frames now keep merge-only local and GC-fact snapshots in a lazy scratch sidecar indexed by nesting depth, while common booleans share one flag word and a write-only loop-growth flag is gone. This reduces `ctrlFrame` from 368 to 312 bytes on ARM64 (-15.2%) and from 408 to 312 bytes on AMD64 (-23.5%). The sidecar grows with maximum live control depth rather than total blocks and is cleared between functions and released with worker scratch before the parallel join. On the generated 128-deep scalar-control benchmark, Linux/AMD64 allocation fell from approximately 209.5 to 190.0 KiB/op (-9.3%) with the same 154 allocations. Candidate-free `many_funcs` saves 64 B/op on ARM64 and roughly 96 B/op on the full Linux/AMD64 compile path. A merge-heavy `json-as` compile saves about 703 B/op on ARM64 while adding one lazy allocation; Linux/AMD64 pays about 512 B/op and one allocation, the bounded cost of avoiding four scanned slice headers in every frame. Serial/parallel corpus parity and deterministic-artifact tests cover the depth-index ownership boundary. Selection depends only on whether semantic merge state exists; no module, function, producer, hash, or corpus identity participates.
20. Custom plugin type pointers and variable-length register bundles now live in a lazy operand sidecar instead of every value's common `storage`. The ordinary storage record falls from 64 to 40 bytes (-37.5%) and `elem` from 112 to 88 bytes (-21.4%) on both architectures; the sidecar is allocated only when custom machine-code lowering actually produces a custom value, is included in the node-resource ledger, cleared between functions, and released before the parallel join. ARM64 `many_funcs` falls from 103,372 to 95,192 B/op (-7.9%) and `json-as` from 398,962 to 341,409 B/op (-14.4%), with allocation counts unchanged. Five 500 ms native ARM64 samples also moved median compile time from 256.96 to 248.83 microseconds on `many_funcs` and from 1.096 to 1.044 milliseconds on `json-as`. Full Linux/AMD64 compile allocation falls from roughly 157.4 to 149.2 KiB/op on `many_funcs` (-5.2%) and from 439.4 to 381.8 KiB/op on `json-as` (-13.1%). Wide custom-value execution tests cover register ownership, spills, multiple results, and sidecar clearing. Lowering and selection are unchanged, and the representation has no workload-identity selector.
21. Operand spill slots now use their natural 32-bit representation instead of a host-width `int`. The same field's two non-spill uses already have exact 32-bit domains: deferred-memory displacements preserve raw `int32` bits, and known GC array lengths are `uint32`; local provenance is bounded by the 16-bit validated-local ceiling. Local/global/function identity indexes remain host-width in this step. Reordering the compact scalar fields reduces `storage` from 40 to 32 bytes (-20.0%) and `elem` from 88 to 80 bytes (-9.1%) on both architectures. ARM64 `many_funcs` falls from 95,196 to 92,380 B/op (-3.0%) and `json-as` from 341,413 to 305,605 B/op (-10.5%) with allocation counts unchanged; the full Linux/AMD64 path shows the same byte reductions, from roughly 149.2 to 146.4 KiB/op and 381.8 to 346.0 KiB/op. Five 500 ms native ARM64 samples moved median compile time from 276.75 to 266.56 microseconds on `many_funcs` and from 1.098 to 1.096 milliseconds on `json-as`. Layout tests pin the representation, while spill, GC-fact, custom-value, corpus-parity, and deterministic-artifact tests cover every use of the narrowed field. This is a representation-only change derived from validated numeric domains; it has no workload selector.
22. The operand-node tag now occupies padding already present beside the deferred-operation metadata instead of forcing seven alignment bytes before `storage`. This field-order-only change reduces `elem` from 80 to 72 bytes (-10.0%) on both architectures without changing any field, value domain, tree shape, or lowering decision. ARM64 `many_funcs` falls from 92,380 to 89,692 B/op (-2.9%) and `json-as` from 305,605 to 294,725 B/op (-3.6%), again with allocation counts unchanged. Exact layout tests make the saving non-regressible. This is unconditional structure packing with no runtime selector of any kind.
23. GC-root shapes, AMD64 GC-fact snapshots, loop analysis, ARM64 loop pins, and ARM64 deferred cold edges now share the existing depth-indexed control sidecar instead of occupying every control frame. This reduces `ctrlFrame` from 312 to 184 bytes on ARM64 (-41.0%) and from 312 to 160 bytes on AMD64 (-48.7%); size guards pin both the hot record and the correspondingly larger cold sidecar. On the generated 128-deep scalar-control benchmark, Linux/AMD64 allocation falls from approximately 160.0 to 111.2 KiB/op (-30.5%) with the same 154 allocations. Ordinary full-compilation screens are essentially flat to modestly lower: `many_funcs` saves 128 B/op on ARM64 and 160 B/op on Linux/AMD64, while `json-as` saves about 2.1 KiB/op on ARM64 and changes by roughly +128 B/op on Linux/AMD64, all with unchanged allocation counts. Sidecar allocation is driven only by actual semantic state, grows with maximum live control depth, and is cleared at the existing lifetime boundary; no workload identity or corpus selector is involved.
24. Forward branch-patch lists now live in that same depth-indexed control sidecar and are allocated only when a branch actually targets a frame. Parameter, result, and base type slices deliberately remain in the common record: a measured prototype that moved them too made every function result reserve a much larger speculative sidecar, so it was rejected. The retained change reduces `ctrlFrame` from 184 to 136 bytes on ARM64 (-26.1%) and from 160 to 136 bytes on AMD64 (-15.0%). The Linux/AMD64 128-deep scalar-control benchmark falls again from approximately 111.2 to 107.4 KiB/op (-3.4%) with the same 154 allocations, for a cumulative reduction from 160.0 to 107.4 KiB/op (-32.9%) across cuts 23–24. Ordinary module screens remain effectively flat: ARM64 saves 48 B/op on `many_funcs` and about 464 B/op on `json-as`; Linux/AMD64 saves 16 B/op on `many_funcs` and changes by roughly +128 B/op on `json-as`, with allocation counts unchanged. This is an unconditional ownership move based on whether an actual control edge needs patching, never on workload identity.
25. The remaining exception-handler pointer now resides in the same lazy control sidecar instead of every scalar frame. EH construction already required cold semantic state, so this removes eight scanned bytes from every `ctrlFrame` without adding an allocation or changing admission. Both architectures move from 136 to 128 bytes per frame; the sidecar grows by the same eight bytes only at bounded control depths where cold state exists. Allocation-count screens remain unchanged: the Linux/AMD64 deep-control fixture saves 112 B/op, `many_funcs` saves 16 B/op on both architectures, and ARM64 `json-as` saves 112 B/op while Linux/AMD64 remains in the same measured byte class. EH, WasmGC, and complete backend tests cover the ownership change. The decision depends solely on the presence of an exception frame, not module or corpus identity.
26. Each control frame now stores one contiguous parameter-and-result type slice instead of two independent slice headers; its existing parameter and result counts provide checked views into that storage. Indexed block signatures are lowered into one backing allocation rather than separate parameter and result arrays, while single-result blocks and function frames keep their existing backing directly. This reduces `ctrlFrame` from 128 to 104 bytes (-18.8%) on both architectures without moving common type state into the larger lazy sidecar. The Linux/AMD64 128-deep scalar-control fixture falls from approximately 107.3 to 100.0 KiB/op (-6.8%) with the same 154 allocations. ARM64 `json-as` saves about 929 B/op, Linux/AMD64 `json-as` saves about 256 B/op, and `many_funcs` saves 16 B/op on both architectures, again with unchanged allocation counts. The representation follows validated block signatures only and has no workload-dependent branch.
27. Retained function summaries now store the validated parameter-plus-local count in 16 bits and body-derived memory/resolver instruction counts in 32 bits, reconstructing native-width counts only in the stack-local `funcHintView`. Validation already caps locals at 65,535, and a function body section cannot contain more counted instructions than its 32-bit byte length, so the narrower domains are exact rather than saturating heuristics. This reduces `funcHints` from 64 to 56 bytes (-12.5%) on both architectures. The 1,024-function sparse-global hint stress case falls by exactly 8,192 B/op, from 125,976 to 117,784 B/op, with the same 20 allocations. Full `many_funcs` compilation falls by 2,048 B/op on both architectures; `json-as` saves about 384 B/op on ARM64 and 384 B/op on Linux/AMD64, with allocation counts unchanged. The representation is unconditional and independent of producer, module, function, or corpus identity.
28. ARM64's retained summary fields are now ordered by alignment, closing padding holes without changing a type, access, branch, or hint policy. The record contains 45 bytes of payload and now occupies 48 bytes instead of 56 (-14.3%); AMD64 already occupies its 56-byte minimum for the current 52-byte payload, so it is deliberately unchanged. The same 1,024-function sparse-global stress case falls by exactly another 8,192 B/op, from 117,784 to 109,592 B/op, with 20 allocations unchanged. Full ARM64 `many_funcs` compilation falls from approximately 87,480 to 85,432 B/op (-2,048) and `json-as` from 290,704 to 290,320 B/op (-384), again with unchanged allocation counts. Timing samples were noisy and showed no attributable cost; the change is pure layout and applies to every ARM64 function summary without workload classification.
29. ARM64's ten retained boolean hints now occupy named bits in one `uint16` flag word. The scanner sets those exact semantic facts in place and consumers test the same bits; there is no secondary summary, lossy encoding, or module-dependent representation. This reduces both the retained `funcHints` and its embedded stack-local view from 48 to 40 bytes (-16.7%). The 1,024-function sparse-global scan falls by exactly another 8,192 B/op, from 109,592 to 101,400 B/op, with 20 allocations unchanged. Full `many_funcs` compilation falls by approximately 4,096 B/op and `json-as` by 512 B/op, again with unchanged allocation counts. Repeated scan and compile timings overlap the previous layout. Flag selection depends only on decoded instruction semantics and module invariants, never workload identity.
30. AMD64 now uses the same exact packed-fact representation for its fourteen retained booleans, including tail-call, jump-table, SIMD, float-constant, and shared-GC-resolver facts. Alignment-aware field order then reduces `funcHints` and its embedded stack-local view from 56 to 40 bytes (-28.6%). Linux/AMD64 `many_funcs` compilation falls from approximately 80,026 to 73,837 B/op (-6.1 KiB) and `json-as` from 277,184 to 276,288 B/op (-896), with 36 and 910 allocations unchanged respectively. The direct byte-backed and AST scanner microbenchmarks retain identical allocation counts and overlapping emulated timings. As on ARM64, every bit is an exact product of decoded semantics or a module-wide invariant; producer and corpus identity never enter selection.
31. The default-off AMD64 `affine-lea` experiment is retired. Its broad paired screen found that disabling it changed execution by -0.07%, improved full compile time by 0.66%, and left compile allocation unchanged; the worst focused disabled penalty was 0.60%. Before deletion, the dedicated four-shape fixture measured 47-byte covered functions versus 50–52-byte ordinary fallback functions, an isolated 3–5-byte saving that did not clear the generated-code gate. Removing the one-level affine recognizer, option binding, catalog entry, environment control, stats event, and experiment-only tests deletes a net 139 source lines and reduces a matched Linux/AMD64 CLI binary by 194 bytes. Because the option already defaulted off, ordinary scaled-index LEA selection and default generated code are unchanged. The deleted matcher examined only bounded expression shape and was not corpus-specific; its removal further reduces alternate production paths.
32. ARM64 operand storage now uses its exact 32-bit domain for local, global, and function indexes as well as the already-packed deferred-memory metadata. The two GC/EH root booleans share unused bits in the existing value-fact byte behind preserving accessors; tests prove that changing semantic facts cannot erase either root bit. Alignment-aware ordering then reduces `storage` from 32 to 24 bytes (-25.0%) and the pointer-rich `elem` from 72 to 64 bytes (-11.1%). Native ARM64 `json-as` compilation falls from 289,763 to 256,129 B/op (-33,634, -11.6%) and `many_funcs` from 81,288 to 80,648 B/op (-640), with 936 and 40 allocations unchanged respectively. A six-pair interleaved GC-off `json-as` screen had overlapping samples and a roughly 0.9% median latency increase; the complete native ARM64 suite passes. Matched `many_funcs`, `json-as`, and `utf-as` code hashes remain byte-identical. The representation is unconditional and all narrowed values come from validated Wasm index bounds or bounded packed metadata; no workload identity or heuristic selector is involved.
33. AMD64 applies the same exact operand-storage representation, including its bounded GC array-length and integer-tee provenance payloads. Root and value-fact writers use the same preserving metadata accessors as ARM64, while consumers reconstruct host-width indexes only at slice and frame-offset boundaries. This reduces AMD64 `storage` from 32 to 24 bytes (-25.0%) and `elem` from 72 to 64 bytes (-11.1%). Emulated Linux/AMD64 `json-as` compilation falls from approximately 276,243 to 242,611 B/op (-33,632, -12.2%) and `many_funcs` from 73,720 to 73,080 B/op (-640), with 909 and 35 allocations unchanged respectively. A six-pair interleaved GC-off `json-as` screen had overlapping samples and a roughly 0.5% median latency increase; the complete Linux/AMD64 suite passes, including exact GC-reference facts and EH. Matched `many_funcs`, `json-as`, and `utf-as` code hashes remain byte-identical. As on ARM64, every narrowed domain is specified by validated Wasm limits or an existing `uint32` semantic API; no workload identity or heuristic selector exists.
34. The default-off AMD64 `tee-spill-elide` experiment is retired. The broad paired screen found only +0.04% execution benefit, a worst focused disabled penalty of 1.12%, 0.13% better compile time when disabled, and no compile-memory change. Its dedicated pressure fixture did remove six spills and 48 frame bytes when explicitly enabled, but that synthetic upside did not qualify the mechanism. More importantly, the default compiler still tagged every eligible scalar tee and rescanned the full operand stack on local overwrites solely to maintain the dormant alias. Removing the recognizer, metadata maintenance, option/configuration surface, and experiment-only tests deletes a net 124 source lines. A six-pair emulated Linux/AMD64 GC-off screen leaves allocation unchanged and moves median compile time by approximately -0.3% on `many_funcs` and -1.6% on `json-as`; these small timing changes are directional evidence, not a native-AMD64 claim. The surviving GC-reference pressure tests now directly cover fact preservation without the unrelated experiment. Default generated code is unchanged, and the deleted path was shape-driven rather than corpus-specific.
35. ARM64's first-64-locals entry-initialization bitmap now occupies the unused high bit of each existing local-hotness sidecar entry instead of eight bytes in every retained function summary. The existing declared-local initialization loop reads those bits directly while it already visits each local, so compilation adds no reconstruction pass or worker field. Hotness arithmetic preserves the metadata bit and saturates at `2^31-1`, far above every selector threshold, and every score consumer masks it explicitly. This reduces ARM64 `funcHints` from 40 to 32 bytes (-20.0%). Full `many_funcs` compilation falls from 80,648 to 78,088 B/op (-2,560) and `json-as` from 256,129 to 255,745 B/op (-384), with allocation counts unchanged. A six-pair interleaved `GOGC=off` `many_funcs` screen was effectively identical at 208.367 versus 208.353 microseconds median. Matched `many_funcs`, `json-as`, `utf-as`, and a dedicated entry-initialized-local function retain byte-identical native code. This is exact metadata packing for every function; neither the representation nor decoding consults workload identity.
36. AMD64 now uses the same score-sidecar representation and direct declared-local-loop decoding, including conservative-root suppression at the point the packed bit is consumed. This reduces AMD64 `funcHints` from 40 to 32 bytes (-20.0%) without adding a pass, allocation, or worker field. Emulated Linux/AMD64 `many_funcs` compilation falls from 73,080 to 70,520 B/op (-2,560) and `json-as` from approximately 242,609 to 242,225 B/op (-384), with 35 and 909 allocations unchanged. Six-pair interleaved `GOGC=off` medians moved from 339.462 to 338.638 microseconds on `many_funcs` and from 1.259345 to 1.259215 milliseconds on `json-as`; these are parity screens under emulation, not native-AMD64 speed claims. Matched `many_funcs`, `json-as`, `utf-as`, and entry-initialized-local native-code hashes remain byte-identical, and the complete Linux/AMD64 suite passes. As on ARM64, every decision is derived from exact per-local scan metadata rather than workload identity.
37. ARM64 parallel function results now store their worker identity, worker-arena range, body length, and internal-entry offset in exact 32-bit fields, matching the already-32-bit adapter and trap metadata. A checked range conversion rejects a per-worker code arena above 4 GiB before appending or narrowing; the normal join reconstructs host-width slice indexes only at use sites. This reduces `funcResult` from 88 to 64 bytes (-27.3%). With four workers, native ARM64 `many_funcs` falls from approximately 222,952 to 216,180 B/op (-6,772, -3.0%) and `json-as` from 730,742 to 728,980 B/op (-1,762), with median allocation counts unchanged. Five-sample compile medians move from 146.636 to 146.520 microseconds on `many_funcs` and from 530.610 to 533.833 microseconds on `json-as` (+0.6%). Deterministic serial/parallel artifact tests cover the compact ownership boundary. The representation is unconditional and rejects unrepresentable resource size explicitly; function contents and workload identity do not select it.
38. AMD64 parallel results now use the same checked 32-bit worker, code-range, body-length, and internal-entry representation, plus checked 32-bit ranges for its per-worker literal pool. This reduces `funcResult` from 104 to 72 bytes (-30.8%) and its exact result-array payload by 9,632 bytes for the 301-function `many_funcs` module and 1,536 bytes for the 48-function `json-as` module. Six-pair emulated four-worker full-compile samples consistently allocated less but were scheduler-noisy, so they are not used as a precise end-to-end byte or latency claim. Boundary tests prove acceptance at `2^32-1` and rejection above it; deterministic serial/parallel artifact tests and the complete Linux/AMD64 suite remain the qualification gates. The compact representation applies to every parallel function and has no code-selection or workload-identity branch.
39. ARM64 local-frame slot indexes now use their exact 32-bit domain instead of host-width integers. Validated Wasm functions contain at most 65,535 locals, while synthetic inline-local frames are rejected against the much smaller native stack-fence headroom before any home is consumed. A generated 4,000-local scalar stress compile falls from 414,098 to 397,714 B/op (-16,384, -4.0%) with the same 38 allocations; `many_funcs` saves 32 B/op and `json-as` 120 B/op because their reusable worker backing falls into nearby allocator size classes. Five-sample stress medians overlap, and matched `many_funcs`, `json-as`, and `utf-as` native-code hashes remain identical. The representation is unconditional and derived solely from validated index and frame-size limits.
40. AMD64 local slots now pack the accepted frame's byte offset into 24 bits and the compact finalizer's bounded reference count into the remaining eight bits of one `uint32`. Native frames are limited to roughly 256 KiB, far below the 16 MiB offset domain; the 256th reference marks the recorder inexact and disables slot reordering rather than wrapping its count. This avoids a speculative count sidecar and halves the backing in both ordinary and compact-finalizer modes. The generated 4,000-local stress compile falls from 494,696 to 478,312 B/op (-16,384, -3.3%) with the same 42 allocations; `many_funcs` saves 32 B/op and `json-as` 120 B/op in both measured modes. Emulated timing samples overlap, the focused slot-order execution suite passes, and matched `many_funcs`, `json-as`, and `utf-as` native-code hashes remain identical. Packing is unconditional and depends only on architectural frame and finalizer limits.
41. GP pin candidates on both backends now store their validated local/global index in 32 bits and place the one-byte kind after the two scalar words, reducing the pointer-free record from 24 to 12 bytes. The ranking, stable tie break, and selected registers are unchanged. Because the reusable candidate slice grows geometrically, a generated 4,000-integer-local compile falls by approximately 137.2 KiB/op on each backend: 397,714 to 260,512 B/op on ARM64 and 478,312 to 341,112 B/op on Linux/AMD64, with allocation counts unchanged. ARM64 `many_funcs` saves 16 B/op and `json-as` 368 B/op; five-sample stress medians improved slightly on both targets, though AMD64 remains emulated evidence. Layout guards pin the 12-byte record, complete focused backend suites pass, and matched `many_funcs`, `json-as`, and `utf-as` hashes remain identical. This is unconditional exact-domain packing, not a pin-admission or workload-selection change.
42. Float/vector pin-candidate indexes now use the validated 16-bit local-index domain instead of host-width integers on both backends. The candidate walk covers only the function's original locals—never appended inline scratch—and validation caps that count at 65,535, so every emitted index is exact. A generated 4,000-`f64`-local compile falls from 226,984 to 123,688 B/op on ARM64 (-45.5%) and from 348,728 to 245,432 B/op on Linux/AMD64 (-29.6%); both also remove three geometric-growth allocations. Native ARM64 median time improves from 112.6 to 109.0 microseconds, while the emulated AMD64 samples overlap. Ordinary `many_funcs` and `json-as` remain in the same allocation classes. Selection and ordering are unchanged, and focused backend suites cover the conversion.
43. Call-staging slot-prefix scratch now uses `uint32` on both backends instead of retaining one host-width integer per live operand. Every successful native frame already fits the much smaller stack-fence bound; rejected oversized functions never publish their transient offsets. A generated synchronous-host-call shape with 1,000 values live below the call falls from 309,029 to 296,741 B/op on ARM64 and from 453,538 to 441,250 B/op on Linux/AMD64, removing one allocation on both targets. Five-sample medians overlap, while ordinary `many_funcs` and `json-as` remain in the same allocation classes. All four call-staging paths reconstruct host-width indexes only at spill-address use sites, and matched `many_funcs`, `json-as`, and `utf-as` native-code hashes remain identical. The representation depends only on the successful frame-size invariant, not call target or workload identity.
44. Operand arenas now retain reusable chunks only within a fixed 1 MiB byte budget per compiler worker and immediately release any suffix above it when a function completes. This supersedes demand-following trim hysteresis: on a 1,000-function regex module, that policy repeatedly discarded and regrew the same bounded backing, accumulating 8.62 MiB of discarded node storage in one compile. Ten interleaved native AMD64 samples with the fixed ceiling reduce full-compile allocation from 9.177 to 1.927 MiB/op (-79.0%), allocations from 5,413 to 5,271 (-2.62%), and time from 77.37 to 75.07 milliseconds (-2.96%); the retained 1,032,192-byte arena stays below the ceiling and incurs zero discarded bytes. Native ARM64 samples reduce regex allocation from 9.226 to 1.952 MiB/op (-78.84%) and `json-as` from 225.2 to 193.2 KiB/op (-14.21%), with timing ranges overlapping and p1–p8 generated code unchanged. Giant functions may still grow without limit for correctness, but their over-budget chunks are cleared and released at the function boundary. The ceiling is a target-independent worker-resource rule and never inspects function identity, ordering, producer, or corpus membership.
45. ARM64 loop scans no longer allocate a Go hash map for every loop merely to remember which locals are assigned. The scanner writes exact validated 16-bit local indexes into reusable function scratch, sorts and deduplicates each loop segment, and control frames retain checked compact ranges into that arena; membership is a binary search. Unknown scan state remains explicitly conservative. Native `json-as` falls from approximately 222,498 to 211,923 B/op (-4.8%) and from 935 to 770 allocations, while `many_funcs` remains in the same allocation class. Five compile samples overlap. Bounds-hoist eligibility and loop-region pinning consume the same exact modified-local set, and neither generated-code policy nor workload identity changes.
46. AMD64 now uses the same sorted 16-bit modified-local scratch for both its ordinary loop scan and the combined loop-versioning scan. Control merges retain an eight-byte range/validity record instead of a hash-map pointer, so `ctrlFrameMerge` remains 232 bytes; exact GC-fact invalidation iterates the compact segment directly. Emulated Linux/AMD64 `json-as` falls from approximately 208,980 to 198,659 B/op (-4.9%) and from 908 to 748 allocations, while `many_funcs` remains in the same allocation class. Five timing samples overlap under emulation. The existing loop-precheck experiment and ordinary bounds-hoist logic consume identical semantic facts; no new selector or corpus-specific path is introduced.
47. Trap-stub patch records now retain function-local branch offsets as checked `uint32` values on both backends, matching their existing function and Wasm-PC fields. This reduces `trapSite` from 16 to 12 bytes; AMD64 reserves the all-ones value for its internal no-common-jump sentinel and rejects it at the narrowing boundary. `json-as` falls by approximately 1,072 B/op on each backend (ARM64 211,923 to 210,851; Linux/AMD64 198,659 to 197,586), with allocation counts unchanged; `many_funcs` has no trap-site backing and remains unchanged. Timings overlap. Stub grouping, patch order, and native code are unchanged, and the representation is selected solely by the explicit per-function offset domain.
48. ARM64 direct-call relocation records now retain their function-local patch offset and local-function target as checked `uint32` values rather than host-width integers, reducing each record from 24 to 12 bytes. The all-ones value is reserved as an invalid test and corruption sentinel; module patching validates the entry table, patch site, target, and internal-entry table before indexing. Native `json-as` falls from approximately 210,841 to 206,929 B/op (-3,912, -1.9%) with 770 allocations unchanged, exactly matching 326 retained relocations times the 12-byte record saving; `many_funcs` has no retained direct-call relocations and remains unchanged. Five timing samples overlap. Call selection, final offsets, and native bytes are unchanged, and compaction applies to every relocation from explicit code-size and function-index domains.
49. AMD64 direct-call and shared-GC-stub relocations now use the same checked 32-bit patch-offset and function-target representation. The one-byte stub kind and internal-entry bit fit beside those fields, reducing `callReloc` from 24 to 12 bytes; stub relocations remain explicitly distinguished before target validation, and malformed metadata uses the same all-ones sentinel. Emulated Linux/AMD64 `json-as` falls from approximately 197,577 to 193,649 B/op (-3,928, -2.0%) with 748 allocations unchanged, while relocation-free `many_funcs` is unchanged. Five timing samples overlap under emulation. Finalization, adapter compaction, patch semantics, and generated code remain unchanged, and all direct calls and shared-stub calls use the same representation regardless of workload.
50. ARM64 operand nodes now encode the physical stack's previous/next links as stable 32-bit arena coordinates rather than Go pointers. The coordinate uses 16 bits each for chunk and slot-plus-one, with zero as nil; chunks are capped at 8,192 nodes and exhausting the 65,536-chunk domain is rejected. This first staged conversion reduces `elem` from 64 to 56 bytes while leaving deferred-child pointers and all tree selection unchanged. Native `json-as` falls from 206,928 to 196,688 B/op (-10,240, -4.9%) and `many_funcs` from 78,040 to 75,992 B/op (-2,048, -2.6%), with allocation counts unchanged. A six-pair interleaved `GOGC=off` `many_funcs` screen had mixed signs and a roughly +0.7% median delta, within the memory-only gate. Tests pin cross-chunk identity and sentinel/reset behavior. Every ARM64 node uses the same representation; there is no function or workload admission path. An identical AMD64 prototype saved the same bytes but regressed interleaved emulated `json-as` compile time by roughly 4% median, so it was removed rather than creating a target-default regression. A second ARM64 prototype also converted child links, reached a pointer-free 48-byte node, and cut `json-as` to 167,792 B/op, but two-level child lookups regressed native GC-off compile time by roughly 3–5%; it too was removed. Full conversion must therefore eliminate lookup indirection structurally—most likely by moving all transient node users to IDs over a flat movable arena—not stack more coordinate lookups onto pointer-oriented APIs.
51. Global-hint eligibility now occupies the unused high bit of the accumulator's per-global epoch word, removing its dense `[]bool` scratch while preserving the independent full-width saturated hotness score. Epoch comparison masks the metadata bit, and the 31-bit epoch rollover explicitly clears all marks before reuse. The 1,024-function/1,024-global sparse-use hint benchmark falls from 93,208 to 92,184 B/op and from 20 to 19 allocations, exactly removing one byte per global and its allocation; eight native ARM64 `GOGC=off` samples overlap. Ordinary `many_funcs` remains in the same allocation class, while `json-as` saves eight bytes. The representation is shared by both backends and applies to every validated global index; no function shape or workload identity selects it.
52. Constant-division magic-number derivation now uses one exact 128-by-64 `bits.Div64` operation instead of allocating several `math/big.Int` values for every qualifying divisor. The construction's largest numerator is `2^127`; its proposed quotient is strictly below `2^64`, and the only doubled intermediate is consumed modulo the selected 32- or 64-bit width. A deterministic test compares the complete result tuple against the former big-integer construction for more than 20,000 random and boundary divisors across both widths. Native ARM64 `json-as` falls from 196,680 to 191,496 B/op and from 770 to 567 allocations; emulated Linux/AMD64 falls from 193,648 to 188,448 B/op and from 748 to 544 allocations. Eight ARM64 and six AMD64 `GOGC=off` timing samples are neutral to favorable. `many_funcs`, which has no qualifying constant divisions, remains unchanged. The helper is architecture-neutral and selected only by the existing constant-division lowering rule.
53. ARM64's retained function summary now uses its last two padding bytes for a saturated count of local direct-call sites. Functions with at least eight such sites reserve their compact 12-byte relocation backing once instead of retaining each geometric append generation; smaller call sets keep ordinary append because an optional inline can erase their only relocation and too few size classes are crossed to repay the reserve. Native `json-as` falls from 191,496 to 190,248 B/op and from 567 to 544 allocations, with six `GOGC=off` timing samples overlapping. `many_funcs` remains exactly 75,992 B/op and 40 allocations after its small/inlined call sets correctly stay below the reserve threshold. The 32-byte summary size and generated code are unchanged. This is a target-record cost rule over a decoded semantic count, not a module, producer, function-identity, or corpus selector; saturation only reduces reservation accuracy and append remains the correctness fallback.
54. ARM64 control merges now move their three exact-GC root slice headers into a lazy sidecar parallel to merge depth. The common merge record falls from 232 to 160 bytes (-31.0%); scalar functions allocate no root sidecar, while a reference-bearing frame obtains the same three slices from one bounded arena whose movement, clearing, worker release, and resource-ledger bytes follow the existing merge owner. Native `json-as` falls from 190,248 to 188,840 B/op with 544 allocations unchanged, and eight `GOGC=off` timing samples overlap; `many_funcs`, which does not allocate merge backing, is unchanged. Focused tests pin the 72-byte root record, depth-slot movement, and clearing. Selection depends only on whether exact semantic root bits exist at structured control, never workload identity.
55. AMD64 applies the same depth-parallel split to all eight exact-GC root and reference-fact slices. Its common `ctrlFrameMerge` falls from 280 to 88 bytes (-68.6%), while the 192-byte GC record is allocated in one bounded arena only when root or fact state actually exists. Merge-slot movement, clearing, worker release, and resource accounting keep both owners synchronized; the four peak/discard counters use exact 32-bit capacity domains so adding the sidecar does not enlarge ordinary scratch. Emulated Linux/AMD64 `json-as` falls from 188,448 to 185,120 B/op with 544 allocations unchanged, while `many_funcs` remains exactly 70,480 B/op and 35 allocations. Six `GOGC=off` timing samples are neutral to favorable, and the complete backend suite passes. The split follows semantic GC state only and introduces no module or workload admission rule.
56. ARM64 now preserves its dense global-register scratch backing when resetting the reusable function compiler instead of dropping and reallocating it for every function. Each one-byte register entry also stores its dirty value-pin flag in the unused high bit, removing the parallel dense `[]bool`; physical AArch64 registers occupy only the low five bits and `regNone` remains an explicit all-ones sentinel. Each function reslices and clears the retained table before installing module and value pins, so no state crosses ownership boundaries. Native `json-as` falls from 188,840 to 186,120 B/op and from 544 to 459 allocations; eight `GOGC=off` timing samples are neutral to favorable, while global-free `many_funcs` remains exactly 75,992 B/op and 40 allocations. Tests cover packed register/dirty decoding, clearing, and allocation-free backing reuse. This is one unconditional scratch lifecycle and representation for every function, not a global-index or workload-specific optimization path.
57. AMD64 applies the same reusable global-register backing and packed dirty flag. Its physical register domain fits in four low bits; all ordinary global accessors, call spill/reload loops, module-boundary synchronization, and GC native-stub preservation loops decode the register before use. Emulated Linux/AMD64 `json-as` falls from 185,120 to 182,400 B/op and from 544 to 459 allocations, exactly matching the ARM64 delta; eight `GOGC=off` timing samples are favorable, while global-free `many_funcs` remains exactly 70,480 B/op and 35 allocations. Packed-state and allocation-free reuse tests pass with the complete backend suite. The ownership and representation are unconditional and shared in design across targets.
58. Direct-call relocations now use one reusable per-function scratch buffer and are copied into flat append-only module or worker arenas instead of retaining a separate backing allocation for every caller. Serial compilation reserves the module arena from the already decoded local-call count only when the target record-size threshold repays the allocation; parallel results retain checked 32-bit ranges into their worker's relocation arena and reconstruct the existing per-function views after the workers stop mutating it. AMD64 packs its saturated eight-bit outgoing count into the unused high byte of the existing resolver-site word, preserving the 32-byte function summary and its independent saturated 24-bit resolver count. Native ARM64 `json-as` falls from 186,120 to 185,816 B/op and from 459 to 397 allocations; emulated Linux/AMD64 falls from 182,400 to 180,849 B/op and from 459 to 374 allocations. `many_funcs` remains exactly 75,992 B/op and 40 allocations on ARM64 and 70,480 B/op and 35 allocations on AMD64 because its below-threshold, optionally erased relocation set does not force an arena reserve. Six-sample ARM64 timing medians remain within roughly 0.7% of the matched baseline, and both complete target suites pass. The mechanism depends only on decoded local-call counts, exact ownership lifetime, and checked arena size; append remains the correctness fallback for saturation or lowering-created helper calls, and no workload identity participates.
59. ARM64 control-edge local-state snapshots now pack the four exact states into two bits each, reducing every merge buffer from one byte per declared local to one byte per four locals. Unlike AMD64's already-compact pinned-local-only snapshots, ARM64 must retain stable local-index addressing because bounded regional pins can enter and leave across nested control; the packed form preserves that identity while keeping the bounded pool and all merge, dead-reload, and versioned-loop semantics. Native `json-as` falls from 185,816 to 185,368 B/op and from 397 to 388 allocations, while `many_funcs` remains exactly 75,992 B/op and 40 allocations. Six `GOGC=off` timing samples are favorable, the focused merge-next-use tests exercise packed source/target reads, and the complete native suite passes. The representation applies to every ARM64 local-state snapshot and is indexed solely by validated local identity; there is no function-shape or workload selector.
60. ARM64 now records maximum structured-control depth in the final alignment byte of its existing 32-byte function summary and uses that exact one-pass fact to reserve the serial control stack once. `json-as` falls from 185,344 to 183,712 B/op and from 387 to 383 allocations in matched binaries; eight 750 ms `GOGC=off` samples overlap, with the median slightly favorable. Straight-line modules remain lazy, saturated or incomplete hints retain append growth as the correctness fallback, and parallel workers cap speculative pointer-rich backing at eight frames so one deeply nested function cannot multiply its full reservation across workers. `many_funcs` remains exactly 75,992 B/op and 40 allocations. The reservation is derived solely from decoded structured-control nesting and has no producer, module, function, or corpus selector.
61. Deferred integer-compare operands now retain the value node that `condense` already rewrites in place instead of allocating a second short-lived node containing the same register. The fused-branch and materialized-boolean paths on both targets use the existing owner, preserving register release and operand-stack consumption exactly. Native ARM64 `json-as` falls from 183,712 to 182,816 B/op and from 383 to 369 allocations; emulated Linux/AMD64 falls from 180,849 to 179,953 B/op and from 374 to 360 allocations, the same 896-byte and 14-allocation reduction. `many_funcs` remains exactly 75,992 B/op and 40 allocations on ARM64 and 70,480 B/op and 35 allocations on AMD64. Five ARM64 and three AMD64 `GOGC=off` timing samples are neutral to favorable. The cleanup applies to every deferred integer compare and removes representation duplication without adding an admission path or selector.
62. Scalar block signatures now keep their sole result type in the control frame's existing `res0` byte instead of allocating a one-element slice. Indexed multi-value signatures retain the contiguous parameter/result slice, while frame reconstruction appends either representation into the same reusable type scratch. Native ARM64 `json-as` falls from 182,816 to 182,608 B/op and from 369 to 227 allocations; emulated Linux/AMD64 falls from 179,953 to 179,713 B/op and from 360 to 218 allocations. `many_funcs` remains in the same 75,992/40 ARM64 and 70,480/35 AMD64 allocation classes. Five ARM64 and three AMD64 `GOGC=off` samples are favorable. Tests pin the nil-slice scalar representation and exact reconstruction, and full corpus coverage exercises blocks, loops, `if`, exceptions, GC roots, and indexed signatures. The representation is selected solely by the Wasm block type's exact arity.
63. Control frames now retain their entry-stack type sequence as an exact 32-bit range instead of a slice header and per-frame backing allocation, reducing the common frame from 104 to 88 bytes on both targets. Ordinary ranges use the unused tail of the existing fixed 64-byte function-result type scratch and release in control-stack order; a frame whose live type depth exceeds that fixed capacity keeps a combined base/signature sequence in its existing cold `types` backing, so correctness never depends on the cap and worker state does not grow. Native ARM64 `json-as` falls from 182,609 to 181,921 B/op and from 227 to 194 allocations; emulated Linux/AMD64 falls from 179,714 to 179,057 B/op and from 218 to 185 allocations. `many_funcs` also saves 16 B/op on each target (75,992 to 75,976 ARM64; 70,480 to 70,464 AMD64) with allocation counts unchanged. Five-sample timing ranges overlap. Tests pin frame size, nested LIFO reconstruction and reuse, out-of-order rejection, and the exact cold fallback. The fast representation is bounded only by an existing fixed resource capacity, and the fallback is selected solely from semantic operand depth—never workload identity.
64. Forward control-edge patch lists now keep their first site inline in a merge word that only loop frames otherwise use; non-loop frames allocate pooled overflow only from the second site onward. Overflow offsets use checked 32-bit storage, and ARM64 packs the conditional/unconditional patch kind into the offset's high bit under the target's existing sub-2-GiB native-function bound, allowing its two former lists to become one. ARM64's common merge sidecar falls from 160 to 136 bytes, while AMD64 remains 88 bytes and both remove the first backing allocation per active end target. Native ARM64 `json-as` falls from 181,921 to 180,656 B/op and from 194 to 179 allocations; emulated Linux/AMD64 falls from 179,057 to 178,161 B/op and from 185 to 169 allocations. `many_funcs` remains exactly 75,976/40 ARM64 and 70,464/35 AMD64. Five-sample timings are neutral to favorable. Focused tests pin first-site and overflow encoding, and complete backend suites cover branches, tables, exceptions, unreachable control, and serial/parallel parity. The representation applies to every forward edge and has no admission policy or workload selector.
65. Non-loop control frames now keep their second forward-end patch in the frame's otherwise-unused `loopStart` word, so pooled overflow begins only at the third edge without adding any state. Native ARM64 `json-as` falls from 180,656 to 180,593 B/op and from 179 to 177 allocations; emulated Linux/AMD64 falls from 178,161 to 178,097 B/op and from 169 to 167 allocations. `many_funcs` remains unchanged on both targets, structure sizes do not grow, and five-sample timing ranges overlap. Focused tests pin both inline entries and third-site overflow. The union is exact from the control kind: loops use the word only as a backward target and never collect forward-end patches, while every non-loop frame uses the same representation regardless of workload.
66. Integer/global and float/vector pin selection now retains only the exact top K candidates that can consume physical registers instead of materializing and sorting every eligible local. Two fixed 32-entry stack arrays cover the architectural register files; insertion preserves the existing descending score, local-before-global, and ascending-index tie order, and explicit invariants reject an impossible target pool larger than the architecture. This removes two reusable slice headers from per-function worker state and makes candidate memory independent of local count. Native ARM64 `json-as` falls from 180,593 to approximately 180,217 B/op and from 177 to 172 allocations; emulated Linux/AMD64 falls from 178,097 to 177,721 B/op and from 167 to 162 allocations. `many_funcs` also removes one allocation (ARM64 75,976 to 75,968 B/op; AMD64 70,464 to 70,448 B/op). Five-sample timings are favorable on ARM64 and overlap under AMD64 emulation. Focused tests pin the complete candidate ordering, and full backend corpus tests preserve generated-code behavior. K is derived only from target register availability and the existing semantic pin budget; no workload identity or body-shape exception exists.
67. Forward returns now form an intrusive compile-time chain inside the unused immediate fields of their unresolved branch instructions instead of growing a separate `[]int`. ARM64 stores the previous word index in the 26-bit `B` immediate and clears it before the encoder's OR-style final patch; AMD64 stores the previous displacement offset in the placeholder's four rel32 bytes. Both use zero as the terminator, validate the exact native-function offset domain, and patch the complete chain before any finalizer or executable publication can observe it. Native ARM64 `json-as` falls from approximately 180,217 to 179,961 B/op and from 172 to 167 allocations; emulated Linux/AMD64 falls from 177,721 to 177,465 B/op and from 162 to 157 allocations. `many_funcs` remains unchanged, and five-sample timing ranges overlap. Focused tests emit, link, patch, and decode three returns on each ISA; complete backend suites cover explicit returns, branches to the function frame, inlining, compaction, and parallel determinism. This is one unconditional representation for all return sites, with no workload selector or retained high-water.
68. ARM64 finalizer peepholes now retain branch-target membership in a pointer-free bitset instead of a growing hash map. A fixed 64-word worker buffer covers 16 KiB of native instructions with one bit per four-byte instruction; a larger function receives one exact temporary backing, and the next ordinary function immediately replaces it with fixed storage rather than retaining giant-function high-water. Finalizer fragments, PC-relative presence, and plugin exclusion are read from their existing canonical inventories, so the old map remains only behind explicit finalizer validation and no longer mixes validation markers with production target state. Native ARM64 `json-as` falls from 179,961 to 170,881 B/op and from 167 to 157 allocations; `many_funcs` falls from 75,968 to 75,304 B/op and from 39 to 35 allocations. Five current samples ran at 825–856 microseconds and 214–220 microseconds respectively; timing is not claimed as a win without a matched interleaved baseline. Boundary tests cover bit positions, invalid targets, the inline/overflow threshold, and replacement after a giant function, and complete native ARM64 and emulated Linux/AMD64 suites pass. Capacity depends only on native instruction count and never on module or workload identity.
69. Control-edge local-state caches now bound pointer-rich headers and pointer-free payload independently on both architectures: at most 4,096 reusable buffers and 128 KiB of backing payload per worker. Explicit power-of-two header growth stops at the exact entry ceiling, so retained pool storage is bounded at 224 KiB per worker; live state beyond either ceiling still allocates exactly what correctness requires and is released when its frame closes. This replaces the earlier sixteen-buffer limit, which looked small but made modules with thousands of tiny branch-table snapshots repeatedly reallocate them: the native ARM64 `esbuild` compile falls from about 105.31 to 18.77 MiB/op (-82.2%) and from 131,219 to 14,916 allocations (-88.6%), with ten interleaved one-shot timings neutral (-0.003%). Native AMD64 falls from 124,860 to 9,343 allocations (-92.5%) and from 20.02 to 19.75 MiB/op; ten interleaved timings differ by +0.26%, inside run noise. Focused tests exercise the independent entry and byte ceilings, exact header capacity, reuse accounting, and undersized eviction. Both limits are fixed worker-resource policy, with no module, function, opcode, or corpus identity in selection. A follow-up per-function contiguous snapshot-arena prototype was rejected: resetting it at each function discarded profitable cross-function reuse for exact state shapes outside the arena admission, raising native AMD64 `esbuild` from about 7,909 to about 18,000 allocations with bytes essentially flat. Any future compaction should therefore change the state representation itself rather than add a second shape-dependent allocation path.
70. Serial compilation now reserves the local-type, frame-slot, local-definition, and enabled GC-fact tables once at the exact maximum local count among functions that will actually be compiled. Those arrays already inevitably grow to that size; using validated function summaries removes source-order intermediate generations without increasing final serial high-water. Standalone bodies omitted by general inlining do not force the reservation, malformed type references remain on the existing explicit-error path, and parallel workers stay demand-grown so a single outlier cannot multiply across workers. Native ARM64 `json-as` falls from 169,985 to 169,824 B/op and from 156 to 144 allocations; emulated Linux/AMD64 falls from 176,569 to 176,377 B/op and from 156 to 144 allocations. `many_funcs` falls by eight bytes and two allocations on both targets, to 75,296/33 ARM64 and 70,440/32 AMD64. Three-sample timing ranges are favorable on ARM64 and overlap under AMD64 emulation. Focused tests pin exact capacities and ensure disabled GC facts allocate no table; complete backend suites cover local initialization, pinning, retries, inlining, and malformed modules. Reservation depends only on validated local counts and ownership topology, never body identity or corpus membership.
71. The shared global-hint accumulator now retains its first 32 unique touched-global indexes in a pointer-free inline array instead of geometrically growing a temporary slice. Functions touching more globals append only the excess to an ordinary dynamic fallback; finalization sorts both sets and performs an exact deterministic merge, preserving the prior index order and every score/eligibility decision. Native ARM64 `json-as` falls from 169,824 to 169,585 B/op and from 144 to 140 allocations; emulated Linux/AMD64 falls from 176,377 to 176,122 B/op and from 144 to 139 allocations. Global-light `many_funcs` remains in the same 75,296/33 ARM64 and 70,440/32 AMD64 classes. An eight-run matched `GOGC=off` binary comparison was neutral at 685.0 versus 685.4 milliseconds for the complete 500 ms ARM64 benchmark command. Focused tests cross the inline boundary and verify globally sorted output. The capacity is a fixed module-scan scratch bound with a complete fallback, not a function or corpus admission path.
72. The producer-shaped `swar-idioms` family has been deleted from both backends. A current full-corpus stats census found `swar-widen4`, `swar-pack4`, and `swar-parse4` only in json-as, utf-as, and their synthetic pack/parse fixture; `mul-high-u64` appeared only in the synthetic xjb-mulhi fixture. With no independent-producer hits, the family failed the plan's generality gate despite its focused execution wins. Removing it deletes bytecode lookahead, recursive tree matchers, four private deferred operations, target emitters, a public optimization switch, and producer-focused tests: 1,621 source lines are removed while the independent packed-mask `TEST`/`TST` selection remains. Five native ARM64 500 ms samples put `utf-as.convertN` at 106.96→122.45 us (+14.5%) and the synthetic combined pack/parse loop at 949→1,230 ns (+29.6%); json-as scalar exports remain within 1%, as do the xjb-mulhi loop and SIMD utf-as exports. Native code grows by 128 bytes in json-as (+0.2%), 48 in utf-as (+1.1%), 124 in the synthetic pack/parse module (+27.2%), and 72 in xjb-mulhi (+17.6%); `many_funcs` is unchanged. A five-sample 200 ms full execution-corpus watchpoint is +0.8% geomean excluding the two synthetic modules, and +0.4% when also excluding the directly affected utf-as scalar export. This is an intentional simplicity/generality trade, not a memory or execution win, and the motivating-workload regressions remain visible.
73. AMD64's already-default-off `v128-sink` experiment has been removed instead of remaining a permanent alternate SIMD lowering path. Its longer native qualification had found no allocation benefit and only a 0.59% SIMD execution geomean benefit, while the implementation added instruction lookahead, local-alias reconciliation, destination-specialized shuffle/binary/memory/rotate branches, a public flag, an environment switch, and focused tests. Default compilation never selected the path, and current emulated AMD64 code-size checks remain exactly identical before and after removal for the generated SIMD i8x16 suite plus json-as-simd, blake-as-simd, and utf-as-simd. The normal direct-result and pinned-local SIMD lowerings remain. This completes the vector-sink removal on both architectures and reduces public policy surface without replacing it with a new selector.
74. The default-off loop-precheck experiment has been removed from both backends instead of retaining a second compiler that emitted complete checked and unchecked copies of eligible loop bodies. Its broad qualification showed only 0.21% ARM64 and 0.11% AMD64 execution benefit when enabled, while disabling it improved full-compile time by 5.36% and 3.05% and reduced compile allocation bytes by 6.38% and 6.09%, respectively. The deletion removes the public option, environment controls, versioned-loop state, precheck candidate analysis, alternate memory lowering, and experiment-specific tests. The ordinary single-pass loop scanner remains because exact GC-fact invalidation and bounded regional allocation consume its semantic modified-local and control-effect facts. Default native code is unchanged because the experiment was already disabled. Selection is simpler and no workload-specific replacement path is introduced.
75. AMD64's opt-in semantic GC-reference fact is now 12 bytes instead of 16 (-25.0%). Its packed type, identity, nullability, freshness, heap class, pointer-free bit, and complete 32-bit known-array-length domain remain exact; splitting the packed word into two 32-bit fields removes alignment padding around the array length. The unused generation field and its whole-local-table plus operand-stack invalidation walks are deleted: no producer ever established a generation, and barrier selection deliberately treated every representable generation as the same conservative slow-barrier state. On a native Ryzen 7 7800X3D stress function with 1,024 reference locals and eight structured-control levels, compile allocation falls from 96.37 to 88.38 KiB/op (-8.29%) with the same 27 allocations, while compile time improves from 86.76 to 83.32 microseconds (-3.97%, ten interleaved samples). Generated code remains exactly 8,220 bytes. This preserves the semantics-general exact-final subtype specialization rather than deleting the whole default-off facts mechanism; storage changes only when that public optimization is explicitly enabled, and neither representation nor selection consults workload identity.
76. AMD64 structured-control roots no longer reserve five dormant GC-fact slice headers in the default facts-off compiler. A root-only frame sidecar is 72 bytes instead of 192 (-62.5%); when semantic facts are explicitly enabled, roots and facts deliberately retain the former single 192-byte allocation so the optional high-value specialization path pays no extra allocation or latency. On native Ryzen, an eight-level live-reference control fixture falls from 30.08 to 28.22 KiB/op (-6.20%) and from 13.88 to 13.12 microseconds (-5.47%), with 27 allocations and 43 generated bytes unchanged. The corresponding root-plus-fact fixture remains 30.29 KiB/op, 36 allocations, 43 generated bytes, and statistically unchanged at 13.38 versus 13.36 microseconds. Ordinary scalar and control screens are also unchanged. Sidecar selection follows only the public GC-fact policy and whether exact roots reach structured control; no function identity or workload shape participates.
77. Recycled forward-edge overflow buffers are now bounded on both targets to eight buffers of at most 256 checked 32-bit sites, capping their retained backing at 8 KiB per worker instead of allowing one many-branch function to establish unbounded module-lifetime high-water. AMD64's opt-in whole-local GC-fact snapshots are independently capped at sixteen buffers of at most 2,048 compact facts, or 384 KiB of retained backing per worker; wider or deeper functions still allocate the exact live state they need and release exceptional buffers when frames close. Recycled fact buffers no longer clear pointer-free scalar contents because the sole allocator overwrites their complete logical length before reuse. On native Ryzen this improves the 1,024-reference-local/eight-level fact compile from 71.06 to 70.31 microseconds (-1.05%) with the same 88.27 KiB/op, 27 allocations, and 8,220 generated bytes; ordinary scalar and control timings and allocations are unchanged. The ceilings are fixed worker-resource policies with allocation as the correctness fallback, never compilation or code-selection admission rules.
78. ARM64's default-off polymorphic immutable-table fast path and its public experimental switch have been deleted. The alternate path bypassed the ordinary indirect-call entry and therefore did not preserve the per-frame stack fence for deeply recursive targets; enabling it could fault the foreign stack instead of producing the required `call stack exhausted` trap. Its earlier broad screen was also execution-neutral (-0.10% with the path disabled), showed no compile-resource advantage, and found only a 1.99% best focused execution result. The removal preserves the general immutable-table proof, monomorphic direct-call specialization, and correct polymorphic indirect-call lowering while deleting a net 43 source/generated lines and shrinking a matched stripped ARM64 runtime by 32 bytes. The full native ARM64 build and test suite passes. This retires an unsafe dormant alternate compiler path rather than replacing it with a workload-specific selector.
79. Inline caller planning no longer allocates a hash map for the common small distinct-callee set, and its callee-to-frame-base map is recycled across functions. The deduplicator performs at most eight stack-resident linear comparisons before switching to the exact map fallback; the reusable base map is capped at 64 entries, while a larger caller receives an ephemeral exact map that cannot establish worker high-water. On native AMD64, six interleaved three-compile `esbuild` pairs reduce 9,342 to 7,908 allocations (-15.4%) and about 20.70 to 20.57 MB/op (-0.7%), with mean time neutral (-0.03%). Native ARM64 reduces 14,915 to 13,481 allocations (-9.6%) and about 19.68 to 19.54 MB/op (-0.7%); ten interleaved one-shot means improve 0.55%. Across the native AMD64 compile corpus, the stack-set half alone reduces the allocation geomean 2.68% and bytes 0.36%; its +0.56% short-sample time geomean remains inside the memory-only gate. Tests force the ninth-target fallback, preserve first-call order and deduplication, prove zero allocations on reused ordinary base maps, and prove oversized plans leave the bounded pool unchanged. Both thresholds are fixed compiler-resource limits with exact fallbacks, never workload selectors.
80. ARM64's default-off legacy GP- and FP-pin allocators have been retired. They were rollback policies, not semantic fallbacks: the GP switch merely withheld X24, X25, and optionally X8 from call-free functions, while the FP switch capped an otherwise eligible function at four pinned float registers. The dated toggle study found the legacy FP allocator 1.94% slower in execution geomean when enabled, including 37.1% slower BLAKE SIMD, 18.1% slower ray tracing, and 8.1% slower n-body results; the legacy GP result was mixed inside its 3.46% sample spread and supplied no compile-memory benefit. Removing both environment switches, public options, policy branches, schema entries, and manifest names leaves all 36 measured module/worker native-code sizes exactly unchanged under the default policy, deletes a net twelve source/generated lines, and shrinks a matched stripped native ARM64 runtime by 64 bytes. The current pin allocator remains one semantics- and register-pressure-driven implementation, with no replacement workload selector.

A follow-up control-frame type-arena prototype was rejected. Replacing each frame's `[]machineType` with a checked 32-bit range reduced `ctrlFrame` from 88 to 72 bytes (-18.2%) and lowered compile bytes by roughly 0.4% across paired native ARM64 and AMD64 screens, but it required an extra lazy allocation on modules with multi-value block types and repeatedly indexed worker scratch during lowering. Native AMD64 `many_funcs/p4` regressed 2.17% across eight final pairs (p=0.028), beyond the memory-only gate, while code bytes remained identical. The complete prototype was removed. A future attempt must eliminate or inline type access structurally rather than trade a scanned frame header for another indirection.

An interleaved three-pair ARM64 `many_funcs` screen with metrics disabled retained 42 allocs/op and 159,001–159,003 B/op; median compile time moved from 215.98 µs to 215.65 µs. This clears the initial zero-overhead screen, but the larger benchmark matrix remains the acceptance authority.

The policy boundary is explicit: production may select work from validated semantics, effects, bounded resource estimates, and target costs. It must never select an optimization from producer identity, module or function names, function indexes, benchmark membership, hashes, or memorized body bytes. A corpus may validate a general mechanism; it may not activate one.

---

## 1. What current Railshot has already solved

Several recommendations that would have been correct a month ago are now obsolete.

| Area | Current state | Consequence for this plan |
|---|---|---|
| Function-body AST removal | Production decoding already retains raw `BodyBytes` and does not materialize instruction trees. | Do **not** build another streaming or AST-removal frontend. |
| Serial native-code copying | Serial compilation now emits directly into the writable code image. This materially reduced heap use while preserving native bytes. | Do not spend more time on serial output ownership. |
| Basic compiler instrumentation | `CodegenStats` already measures code bytes, frame size, spills, reloads, flushes, bounds checks, calls, pins, peepholes, and unpinned retries. It is opt-in and nil-safe when disabled. | Extend the existing dashboard; do not invent a parallel telemetry system. |
| Initial arena sizing | The September 1 work already uses bounded body estimates for serial compilation and measured up to 18.3% lower backend bytes on `utf-as`. | The remaining issue is retained high-water and node representation, not merely initial capacity. |
| Regional local allocation | Both backends already have bounded interval-region pinning. Current admission is mostly call-free, bounded, straight-line code. | Extend regions only where counters prove it worthwhile. |
| Deferred-load alias handling | The cheap opportunity was measured as nearly dead across the real corpus. | Do not prioritize a general alias-analysis project. |
| Direct parallel image assembly | Multiple implementations reduced Go heap but regressed parallel latency, so the heap-backed join was deliberately retained. | Fix scheduling and scratch coexistence instead of retrying the same mapping design. |

That leaves four current structural cliffs.

### The hot operand node is much too large and too scannable

At the reviewed snapshot, the common `elem` was 112 bytes on both architectures. The reconciliation work above has moved its rare custom-type pointer and register slice into a lazy sidecar, narrowed spill-slot payloads and indexes, packed root metadata, and removed alignment waste. This reduces it to 64 bytes on both architectures. Its four direct `*elem` links remain the next source of size, pointer scanning, and pointer-stability constraints.

Because links are pointers, nodes need stable addresses, which in turn encourages chunked arenas. Reset rewinds the arena; the function boundary retains reusable backing up to a fixed 1 MiB per-worker ceiling and immediately discards any giant-function suffix above it. The pointer-rich representation itself remains the larger structural cost.

Pointer-free backing matters independently of raw byte size: Go does not scan the backing storage of pointer-free slices, maps, or channels. The repository’s own controlled probe found that a 56 MB pointer-rich node backing contributed almost the same amount to `/gc/scan/heap`, while an equally sized pointer-free backing contributed almost none.

### The hint plane retains too much per-function structure

At the reviewed `b40f0305` snapshot, `funcHints` was 200 bytes per function before its referenced local/global score and last-use arrays. The reconciliation work above has since reduced the retained record to 32 bytes on both architectures and moved variable-length data into contiguous module-owned sidecars.

At that snapshot, module hint construction allocated local-score storage proportional to the sum of locals and a dense global-score and eligibility matrix when `functions × globals <= 1<<20`. The dense retained matrix has since been removed; this estimate records the eliminated cliff:

```text
1,048,576 × 4 bytes  global scores
1,048,576 × 1 byte   eligibility
≈ 5 MiB
```

The full dense information is useful while scanning a function, but almost none of it needs to remain attached to every function until compilation ends.

### Register exhaustion can duplicate a whole function’s work

Current code can abandon a pinned compilation attempt after register exhaustion and compile the entire function again with local pinning disabled.

That is a bad p99 cliff even when it is rare:

```text
latency  = failed decode/lower/emit/finalize attempt
         + successful second attempt

memory   = high-water created by both attempts
```

The September 1 arena investigation explicitly identifies retry-cost counters and a bounded pressure hint as the next work.

### Parallelism is bounded by worker count, not by bytes in flight

Four workers produced strong backend speedups, but representative allocated bytes rose by roughly 43–93%, and an `esbuild` run increased peak RSS from about 147 MiB to 190 MiB. Higher worker counts showed even larger backend speedups, but the full-pipeline and memory results are why adaptive mode stops at four.

The next scheduler needs to understand that four tiny functions are different from four giant functions.

---

# 2. Target architecture

The intended end state should look like this:

```text
validated byte-backed module
        │
        ▼
single fused function-summary scan
        │
        ├── compact fixed FuncSummary[]
        ├── compact top-local/global side tables
        ├── root-plan estimates
        └── scratch/code-size/pressure estimates
        │
        ▼
memory-budgeted function scheduler
        │
        ▼
bounded worker state
        ├── pointer-free ValueNode arena
        ├── NodeID operand stack
        ├── exact near-future use within packet
        ├── small fact tables
        └── bounded machine window
        │
        ▼
costed selection + allocation + encoding
        │
        ▼
existing finalizer / relocations / stubs / code-image owner
```

The important architectural constraints are:

- No whole-function SSA.
- No permanent second public compilation tier.
- No per-instruction heap allocation.
- No optimizer structure that grows without a hard cap.
- No retry that recompiles an already emitted function.
- No extra body walk unless its measured benefit pays for it.
- No new compiler mechanism without deleting or replacing old state.
- No transformation whose bounds-check, address, type-width, trap-order, or clobber obligations cannot be stated explicitly.

V8 Liftoff demonstrates that direct one-pass compilation can still keep a virtual stack, defer constants, and snapshot merge state without building an IR. Winch similarly avoids an IR and complex register allocation, but its lower generated-code quality illustrates the ceiling of opcode-at-a-time selection. TPDE points to the useful middle ground: combine selection, allocation, and encoding in one bounded compilation pass instead of serializing those decisions into separate global phases.

---

# 3. Phase 0: turn the current stats system into a resource ledger

This must be the first PR because several later choices depend on distinguishing live memory from allocation traffic, and payload work from GC overhead.

## Extend, do not replace, `CodegenStats`

Add a module-level `CompileStats`, still behind a nil pointer or explicit diagnostics mode:

```go
type CompileStats struct {
    StageNanos       [compileStageCount]uint64
    BodyBytesWalked  [bodyPassCount]uint64
    FunctionAttempts uint64

    HintHeaderBytes   uint64
    HintSidecarBytes  uint64
    RootAnalysisBytes uint64

    WorkerScratchReserved  uint64
    WorkerScratchPeak      uint64
    WorkerScratchRetained  uint64
    WorkerScratchDiscarded uint64

    TransientCodeBytes uint64
    RetainedCodeBytes  uint64
    JoinBytes          uint64

    RetryFunctions      uint64
    RetryInputBytes     uint64
    RetryNodesAllocated uint64
    RetryCodeBytes      uint64
    RetryNanos          uint64

    MaxGPPressure    uint16
    MaxFPPressure    uint16
    MaxV128Pressure  uint16
    MaxFixedPressure uint16
}
```

Also extend per-function stats with:

- Flush reason: call, merge, alias, safepoint, tree cap, machine-window cap.
- Maximum live operand nodes.
- Maximum pending-tree register demand.
- Fixed-register conflict count.
- Pin relinquishments.
- Rematerialization candidates and hits.
- Move-cycle count at calls and merges.
- Selector candidates examined.
- Matcher and machine-window time.
- Root-plan mode and estimated versus actual bytes.

## Measure Go scanning, not only `B/op`

Record these official runtime metrics around complete compilation:

```text
/gc/scan/heap:bytes
/gc/scan/stack:bytes
/gc/heap/live:bytes
/gc/heap/goal:bytes
/cpu/classes/gc/mark/assist:cpu-seconds
/cpu/classes/gc/mark/dedicated:cpu-seconds
/cpu/classes/gc/mark/idle:cpu-seconds
```

Go exposes these specifically so heap scan and GC assist can be separated from ordinary execution CPU.

Run every memory experiment in three modes:

| Mode | Purpose |
|---|---|
| `GOGC=off` | Exposes allocator and compiler payload costs without concurrent GC noise. |
| Default GC | Measures real end-to-end latency and assist cost. |
| Forced GC after compile | Reveals retained and scannable high-water after transient work should be dead. |

## Required benchmark matrix

Use prebuilt binaries and interleaved A/B order. Capture p50, p90, p99, maximum, confidence interval, bytes/op, allocs/op, peak live heap, scan heap, and fresh-process RSS.

The matrix should include:

- `tiny`, `fib`, and one-function scalar modules.
- `many_funcs`.
- `json-as`, `utf-as`, and BLAKE/SIMD.
- Lua, SQLite, Ruby, and esbuild.
- One function with thousands of ALU nodes.
- One function with many locals.
- One module with many functions × many globals.
- Deep structured control.
- Multi-result calls.
- GC-reference-heavy functions.
- Exception handling.
- Plugin/custom-instruction lowering.
- A deliberately register-hostile function.
- A giant function followed by many tiny functions.
- The same giant function compiled repeatedly in one process.
- Workers `1`, `2`, `4`, and `8`.

The **giant-then-tiny** case is essential. Ordinary `B/op` will not reveal a worker that permanently retains a giant scannable arena.

---

# 4. Phase 1: compact the function-summary plane

This is the best low-risk next memory project. It changes representation but need not change generated code at all.

## 4.1 Split `funcHints` into a fixed header and contiguous side tables

Target a 48- or 64-byte fixed record:

```go
type FuncSummary struct {
    Flags uint32

    BodyBytes     uint32
    StackNodeHint uint32
    CodeByteHint  uint32
    LocalCount    uint32

    LocalTopStart  uint32
    GlobalTopStart uint32
    IntervalStart  uint32

    LocalTopCount   uint8
    GlobalTopCount  uint8
    MaxControlDepth uint8
    PressureClass   uint8

    MaxGPNeed       uint8
    MaxFPNeed       uint8
    MaxV128Need     uint8
    FixedReserve    uint8

    // Remaining compact scalar fields.
}
```

A 64-byte header would reduce the fixed record from 200 bytes by **68%** before sidecar savings.

Keep optional information in module-owned pointer-free arrays:

```go
type RankedLocal struct {
    Index uint32
    Score uint32
}

type RankedGlobal struct {
    Index uint32
    Score uint32
    Flags uint32
}

type IntervalLocal struct {
    Index   uint32
    LastGet uint32
    Score   uint32
}
```

Each function stores only an offset and count.

This removes multiple slice headers from each summary, improves locality, and lets all ordinary summaries occupy one non-scannable backing array.

## 4.2 Reuse dense scan scratch; retain only decisions

During the byte-backed hint scan, use one worker-independent scratch object:

```go
type HintScratch struct {
    LocalScore   []uint32
    LocalLastGet []uint32
    LocalEpoch   []uint32

    GlobalScore []uint32
    GlobalFlags []uint8
    GlobalEpoch []uint32

    Epoch uint32
}
```

Resize it only to the maximum local/global count encountered, not the sum across all functions. Use epoch tagging instead of clearing every entry between functions.

For a giant outlier:

- Allocate an ephemeral scratch backing.
- Produce the compact summary.
- Discard the outlier backing immediately.
- Keep the normal reusable backing at a configured cap.

The existing `GlobalHintAccumulator` already follows the right conceptual model: dense reusable scratch during scanning, sparse retained records afterward. Apply that model to locals as well.

## 4.3 Retain exact top-K candidates

Do not approximate pin decisions.

While the full score array is available, retain exactly the candidates that codegen could select:

```text
KGP = number of target GP pin registers + 2
KFP = number of target FP pin registers + 2
```

For small K, an insertion-sorted fixed array is likely cheaper than sorting all locals or maintaining a heap.

Retain full `score + lastGet` information only for functions that pass the existing interval-region admission. That path is already bounded to at most 256 locals and 16 KiB bodies.

## 4.4 Remove the dense function × global matrix

The current matrix can approach 5 MiB at its cutoff. Replace it with:

1. One reusable dense accumulator for the function currently being scanned.
2. Exact extraction of its top global candidates.
3. A compact module aggregate for module-global pin decisions.
4. Per-function sparse records only for globals that survive selection.

This should preserve pin choices exactly because full scores still exist at selection time; only dead information is discarded.

## Phase 1 acceptance gates

- Native code and serialized artifacts remain byte-identical.
- `FuncSummary` is at most 64 bytes.
- No per-function slice allocation for ordinary scalar functions.
- Dense function × global retained storage is gone.
- At least 50% lower summary/sidecar bytes on a many-functions fixture.
- At least 10% lower full-compile `B/op` on a 1,000+ function module.
- No compile-latency regression above the repository’s normal investigation threshold.
- The giant-local scratch is absent after forced GC.

---

# 5. Phase 2: eliminate whole-function retry

The goal is not merely to make retry faster. The goal is to make it disappear from production.

## 5.1 Measure the failed attempt

Use the existing `UnpinnedRetry` event, but preserve the failed attempt’s resource counters before resetting function stats:

```text
failed attempt nanoseconds
failed code bytes
failed node count
failed spill count
pins selected
maximum simultaneous GP/FP/vector demand
fixed-register operation at failure
Wasm byte offset of failure
```

Group failures by cause. Do not assume “too many pins” until the counters prove it.

## 5.2 Compute register demand during the existing hint scan

Add a bounded Sethi–Ullman-style pressure calculation to the byte scan. For each deferred scalar expression:

```text
need(leaf/rematerializable constant) = 0 or 1
need(noncommutative a op b)          = max(need(a), 1 + need(b))
need(commutative a op b)             = min(
                                          max(need(a), 1 + need(b)),
                                          max(need(b), 1 + need(a))
                                      )
```

Track GP, FP, and vector classes separately. Add reserves for:

- Fixed-register instructions.
- Call argument and result staging.
- Memory address temporaries.
- Multi-result values.
- GC helper lowering.
- Bulk-memory clobbers.
- Inline expansion.

This is not a live-interval analysis. It is a conservative register-demand summary of the bounded trees Railshot already constructs.

## 5.3 Choose the pin budget before emission

For each register class:

```text
available_for_pins =
    allocatable
  - predicted_expression_need
  - fixed_register_reserve
  - safety_margin
```

Then select only that many pins. A function with very high temporary demand should begin with fewer optional pins instead of discovering this after emitting most of its code.

## 5.4 Add controlled pin relinquishment

The second defense should be an in-place escape hatch:

1. Select the least valuable optional pin.
2. Write its current value to its canonical local home.
3. Change the local from pinned to frame-resident.
4. Return the register to the allocator.
5. Continue compilation.

This must initially exclude pinned values whose coherence rules are complicated by calls, globals, GC references, or pending borrowed addresses.

This converts a catastrophic full retry into one store and a local-state transition.

## 5.5 Keep retry only as an oracle

For one development cycle:

- Production uses predictive pin budgeting.
- Debug/CI can run the old retry-capable path.
- A test fails if production reaches retry on the corpus.
- Differential tests compare semantics and aggregate quality.

Remove the full second attempt once the pressure corpus and real corpus show zero retries.

## Phase 2 acceptance gates

- Zero production retries on the complete corpus and adversarial pressure suite.
- At least 95% agreement between predicted pressure class and observed maximum pressure.
- No more than 1% execution geomean regression from reduced pinning.
- No individual hot benchmark more than 2% slower unless compile p99 improves enough to justify it.
- Register-hostile compile p99 materially lower.
- Failed-attempt bytes and nanoseconds become zero.

---

# 6. Phase 3: control worker high-water and bytes in flight

Do not change the current 256-node parallel initial arena indiscriminately. The September 1 study deliberately kept that ceiling to avoid multiplying a giant hint across every worker.

The remaining work is lifecycle and scheduling.

## 6.1 Split retained and ephemeral arena chunks

Classify arena backing into:

```text
base chunks       retained across ordinary functions
overflow chunks   owned by one function and discarded at its end
```

A possible policy:

```text
retain up to:
    max(default base, rolling p90 ordinary-function demand)

discard:
    chunks above retain limit
    chunks allocated for one giant function
    chunks whose utilization stayed below 25%
```

Use a fixed byte ceiling so moderately large functions reuse their backing regardless of ordering, while giant-function overflow remains ephemeral.

Because pointer stability is only needed during a function’s compilation, overflow chunks can be dropped immediately after that function completes.

The implemented first lifecycle step retains at most 1 MiB of operand-node backing per worker. Backing within that ceiling survives arbitrarily small intervening functions, avoiding allocation behavior that depends on function order. A function may allocate beyond the ceiling, but after its code is complete the shrink path clears retained chunks, register-user tables, root scratch, and deferred-argument scratch to capacity before dropping every over-budget slice header, so stale `*elem` links cannot keep giant overflow reachable. The compile-resource ledger reports initial node reservation, the sum of per-worker peak envelopes, final retained bytes, and cumulative discarded bytes.

The previous two-function hysteresis failed on a 1,000-function regex module: its alternating demand accumulated 8.62 MiB of discarded node storage even though peak backing was only 1,032,192 bytes. Replacing it with the fixed ceiling reduces native AMD64 allocation by 79.0% and compile time by 2.96% in ten interleaved samples; the equivalent ARM64 allocation reduction is 78.84%, with no detected timing regression. Direct arena tests prove that recurring demand below the ceiling reuses identical backing and that a giant function releases its over-budget suffix immediately. Full giant-lane scheduling and the weighted byte semaphore remain future Phase 3 work.

## 6.2 Add a dedicated giant-function lane

Do not allow four workers to independently become giant workers.

Classify a function as giant when its estimate exceeds any of:

- Node scratch threshold.
- Root-analysis threshold.
- Predicted code bytes.
- Local/global scratch threshold.
- Configured fraction of compile-memory budget.

Only one giant function may compile at once. Ordinary workers continue processing small functions if the remaining budget permits.

This directly protects against permanent multiplication of high-water capacity.

## 6.3 Schedule expensive functions early

Within deterministic constraints, schedule functions approximately largest-first.

The output is still stored by source index, and errors are still reported according to source order. Scheduling order therefore need not affect artifact determinism.

Large-first reduces this overlap:

```text
already-retained output from many small functions
+ giant scratch
+ giant root graph
+ giant temporary output
```

The giant’s scratch is allocated before much output has accumulated, then discarded before the long tail of small functions.

## 6.4 Use a weighted memory semaphore

Estimate per-function transient cost:

```text
reservation(f) =
    fixed worker state
  + node scratch estimate
  + hint/locals scratch
  + root-analysis estimate
  + relocation/finalizer scratch
  + transient function output
```

Permit a function to start only when:

```text
sum(active reservations)
+ retained completed output
+ module fixed memory
<= compilation memory budget
```

Tokens have different lifetimes:

| Token | Release point |
|---|---|
| Operand/node scratch | End of function |
| Root-analysis scratch | Root plan finalized |
| Finalizer scratch | Function artifact finalized |
| Function output | Parallel join or module completion |
| Module summary storage | End of compilation |

If an estimate is exceeded, acquire additional tokens before growing. Underestimation must reduce parallelism, never reject a valid module.

## 6.5 Keep the current parallel heap join

The code-image work already tested direct mapping and disjoint population. Variants reduced heap from roughly 6.25 MB to 3.73 MB on the benchmark but regressed latency by 2.4–12.9%, so they were removed.

Do not revisit that unless another architectural change removes the page-touch or synchronization cost. The scheduler should count the current join’s bytes, not wish them away.

## Phase 3 acceptance gates

- Prediction error within 25% at p90.
- Giant functions never enlarge every worker.
- Forced-GC retained scratch after giant-then-tiny is near the ordinary baseline.
- At the same worker count: at least 20% lower peak RSS on one parallel large-module workload; **or**
- At the same memory budget: at least 15% better backend throughput.
- Serial compilation remains statistically unchanged.
- Serial and parallel native bytes remain identical.
- Error selection remains deterministic.

---

# 7. Phase 4: make GC-root planning adaptive

Current exact root analysis has a 64 MiB arena ceiling. Exact liveness is worthwhile for reference-heavy functions, but it is unnecessary overhead for many functions.

Use three primary modes.

## 7.1 `RootNone`

Select when the function has no references live across any safepoint.

Output:

- No liveness graph.
- No root bitmap.
- No reference-home initialization solely for root reporting.

This should cover most scalar functions.

The first behavior-preserving step is already in place at the control-frame level: root-flag backing is allocated only after an actual reference root appears. An all-scalar frame retains `nil`, including branch-result tracking, while the existing exact per-position representation remains unchanged once any root is present. This removes one allocation per nested scalar block in the dedicated deep-control stress benchmark; it does not yet replace whole-function root analysis.

## 7.2 `RootAllCanonical`

Select for small reference sets where conservative canonical roots are cheaper than constructing and solving a graph.

Requirements:

- Every root slot included in the bitmap contains either a valid reference or canonical null.
- Reference operands are canonicalized before a safepoint.
- No uninitialized frame bytes are interpreted as handles.
- The cost of extra root retention stays below a fixed threshold.

A simple model is:

```text
conservative cost =
    prologue initialization bytes
  + safepoints × bitmap words
  + estimated additional objects retained

exact cost =
    graph allocation
  + graph propagation
  + exact-map serialization
```

Choose conservative roots only when the former is clearly cheaper.

This mode may use one function-level local-root bitmap plus small exact stack-home maps where necessary.

## 7.3 `RootExactGraph`

Retain the existing exact analysis for:

- Many reference locals.
- Many safepoints.
- WasmGC-heavy functions.
- Loops where conservative roots would retain substantial object graphs.
- Exception-handling shapes with complex merges.
- Any function outside the simpler modes’ proof obligations.

## 7.4 Optional capped effect tape

Only after measuring graph setup as a dominant cost, prototype a pointer-free byte-backed effect tape:

```text
local-def
local-use
stack-ref-push
stack-ref-pop
safepoint
control-open
control-merge
```

The tape must have a byte cap. If exceeded, use the existing graph analysis. Do not permit it to become a second unbounded representation of the whole body.

## Phase 4 acceptance gates

- Exact root maps remain the differential oracle.
- Full WasmGC, EH, and malformed-module suites pass.
- No invalid or uninitialized slot can be reported as a root.
- Conservative mode’s retained guest-heap increase stays below its configured threshold.
- Root-analysis peak bytes decrease on mixed scalar/reference corpora.
- Ordinary scalar functions allocate no root-analysis graph.
- No runtime GC regression above 1.5% on reference-heavy benchmarks.

---

# 8. Phase 5: replace pointer-rich `elem` with a compact ID representation

This is the largest structural memory change and should be staged rather than rewritten all at once.

## 8.1 First split cold storage

Move rare fields out of the common node:

- Custom instruction/type pointers.
- Variable-length register bundles.
- Plugin-specific payloads.
- Rare GC/type metadata.
- Large immediates that do not fit inline.
- Debug-only source information.

Use a compact `ColdID`:

```go
type ColdID uint32

type ColdValue struct {
    Custom *coreplugins.CustomType
    VRegs  []Reg
    // Other rare fields.
}
```

Ordinary scalar nodes should never pay for pointer-bearing cold fields.

## 8.2 Replace pointers with IDs

A realistic 32-byte common node is:

```go
type NodeID uint32

type ValueNode struct {
    Op    uint16
    Type  uint8
    Flags uint8

    A NodeID
    B NodeID

    Imm uint64

    Aux  uint32
    Home uint32
}
```

That is 28 bytes of fields and will generally occupy 32 bytes after alignment.

Replace:

```go
prev, next, arg0, arg1 *elem
```

with:

```go
Prev, Next, A, B NodeID
```

Or, preferably, eliminate the intrusive list entirely and represent the operand stack as:

```go
stack []NodeID
```

A reduction from 112 bytes to 32 bytes is a **71.4% reduction in common node payload**. It does not imply a 71.4% reduction in total compiler memory, but it attacks one of the hottest structures directly.

## 8.3 Use one growable pointer-free backing

IDs remain valid if a slice backing moves. That means the current requirement for pointer-stable geometric chunks disappears.

Initially:

```go
nodes []ValueNode
```

can simply grow. The backing is pointer-free and therefore not scanned by Go.

Later, once equivalence is established:

- Add an inline small backing.
- Recycle dead nodes.
- Bound pending nodes per packet.
- Drop giant transient backing after the function.

Do not begin with a custom off-heap allocator. Plain pointer-free Go memory receives most of the GC benefit while preserving portability and tooling.

## 8.4 Introduce node generations only in debug mode

To catch stale IDs during migration:

```text
NodeID = generation | index
```

In release mode, retain a plain compact index unless measurements show the check is free.

## 8.5 Bridge through accessors

The first NodeID PR should preserve all current lowering behavior:

```go
func (f *fn) node(id NodeID) *ValueNode
func (f *fn) left(id NodeID) NodeID
func (f *fn) right(id NodeID) NodeID
```

Machine code should remain byte-identical.

Only after this bridge passes should the allocator and selector begin exploiting compact metadata.

## 8.6 Bound the pending packet

The eventual model should have:

```text
default live pending nodes: 32
hard live pending nodes:    64
```

When the cap is reached:

1. Pick a safe root using deterministic policy.
2. Materialize it to a register or canonical spill home.
3. Replace its subtree with a compact leaf.
4. Recycle unreachable node IDs.
5. Continue.

Cap exhaustion should produce canonical code, not allocate an arbitrarily larger optimizer structure.

## Phase 5 acceptance gates

- Common node at most 32 bytes.
- No pointers in the common node backing.
- At least 60% lower node backing bytes on expression-heavy functions.
- Large reduction in `/gc/scan/heap` attributable to Railshot scratch.
- No per-op heap allocation.
- No native-code differences in the bridge PR.
- No more than 2% compile-latency regression during the bridge.
- Hard packet cap demonstrated by an adversarial function.
- Cap exhaustion does not retry or fail compilation.

---

# 9. Generated-code quality lane

This can begin with smaller work while the NodeID migration is underway, but the larger selector should land on the compact representation to avoid rewriting it twice.

## 9.1 Ship restricted pending `local.set` and `local.tee`

This is already identified in Wago’s roadmap and remains a sound bounded optimization.

Initially admit only values that are:

- Pure.
- Non-trapping.
- Register-free.
- Made from constants, canonical slots, immutable globals, or safe local references.
- Free of borrowed memory addresses.
- Free of GC ownership or rooting complications.

A pending binding enables:

```text
local.set x; local.get x     → forward the pending value
local.set x; local.set x     → delete the first set
local.set x; return          → delete dead store when x is not observable
expr; local.tee x; consumer  → sink directly into consumer and x’s home
```

Flush pending bindings on:

- Control merges.
- Calls and host transitions.
- Safepoints.
- Potentially aliasing local or global effects.
- EH boundaries.
- Plugin/custom-instruction boundaries.
- Packet cap.

This is a better near-term bet than further deferred-load alias work, which existing counters found nearly absent.

## 9.2 Cost entire deferred trees at their sink

Railshot already pays to retain bounded expression trees. It should make one selection decision for the tree rather than a sequence of locally greedy decisions.

Label each root with possible result forms:

```text
immediate
GP register
FP/vector register
flags
memory operand
scaled address
canonical spill home
fixed-register result
```

Each candidate has hard constraints and a cost tuple:

```text
validity:
    type width
    effects
    aliasing
    trap order
    fixed registers
    clobbers
    feature set
    bounds proof

cost:
    temporary registers
    critical-path latency
    estimated uops
    code bytes
    moves
    spills/reloads
    compile effort
```

Selection should be lexicographic at first:

1. Semantically valid.
2. Fits register budget without avoidable spill.
3. Lowest estimated latency/uops.
4. Lowest code bytes.
5. Deterministic tie break.

Avoid one mysterious weighted score until the components have been validated independently.

## 9.3 Use Sethi–Ullman ordering only where semantics permit

For commutative, pure, non-trapping subtrees, choose the child order that minimizes temporary register demand.

Do **not** reorder:

- Potentially trapping loads.
- Calls.
- Atomic operations.
- GC allocation or barriers.
- Volatile/plugin effects.
- Expressions whose trap order is observable.
- Floating-point expressions where a transformation changes required semantics.

## 9.4 Add bounded rematerialization

Rematerialization is the principle that recomputing a cheap value can cost less than spilling and reloading it.

Start with an explicit whitelist:

- Integer constants.
- Zero values.
- Small add/sub constants.
- Shifted indices.
- Masks.
- Known linear-memory base plus small offset.
- Immutable global values.
- Simple extended/truncated local values when the source remains available.

Store a compact rematerialization recipe rather than an owned register. Include actual target cost:

```text
remat cost < spill store + later load + register-pressure penalty
```

Never rematerialize a trapping load or anything whose value can change.

## 9.5 Improve calls and merges before building a global allocator

Several measured slow functions had zero ordinary spills, indicating that generic spill policy is not the primary problem. The valuable traffic is often:

- Call-local reloads.
- Argument shuffles.
- Merge-state shuffles.
- Values pinned across regions where the register would be more useful temporarily.

Add a bounded parallel-move resolver for calls and control merges:

1. Eliminate identity moves.
2. Schedule acyclic moves.
3. Break cycles with one scratch register when available.
4. Otherwise use one scratch slot.
5. Prefer changing a flexible destination assignment over introducing a move.

Record cycles, scratch-register use, and scratch-slot use in `CodegenStats`.

## 9.6 Expand interval regions carefully

Current interval-region pins are bounded to call-free, mostly straight-line functions. Expand in this order:

1. Unique-predecessor structured blocks.
2. Simple `if` regions with identical local state at both exits.
3. Loops with no calls and a statically bounded set of modified locals.
4. Calls whose clobber set does not overlap the admitted cache registers.

Use versioned local-state logs:

```text
local index
old location
new location
definition epoch
```

Cap the log. On overflow, canonicalize the region and continue.

Do not construct whole-function live intervals.

## 9.7 Add a tiny post-selection machine window

Keep the last 8–16 symbolic machine operations before final encoding.

Optimize only exact local patterns:

- Redundant moves.
- Move chains.
- Extension followed by truncation.
- Compare materialization followed by branch.
- Spill immediately followed by reload.
- Duplicate address construction.
- Fixed-register shuffles.
- Load/store cancellation where aliasing is exact.
- Adjacent constant materialization.
- ARM64 address-mode and pair-load/store opportunities.
- AMD64 memory-source and three-operand opportunities.

Flush the window at:

- Labels and branch targets.
- Calls.
- Traps and safepoints.
- EH edges.
- Atomics.
- Plugin/custom effects.
- Relocation boundaries.
- Window cap.

This is a bounded machine peephole buffer, not a basic-block IR.

## 9.8 Generate selection matchers offline

QBE’s `mgen` is a useful model: patterns are numbered offline, candidate sets are represented compactly, and a small generated matcher program captures variables while handwritten policy chooses among valid candidates. Classic `wburg` work shows that useful tree grammars can be optimally parsed in one bottom-up pass and can even avoid an explicit full IR tree.

A Railshot rule should declare:

```text
root opcode
child classes
input/output types
result form
effects
trap behavior
alias class
fixed registers
clobbers
required CPU features
bounds-proof transformation
emitter
cost tuple
```

Runtime limits:

```text
maximum tree depth:       8
maximum bytecodes/root:  32
maximum candidates/root: 8
maximum alternatives:    2 per goal
online allocations:      0
```

Keep direct switch fast paths for singleton patterns. Invoke the matcher only where an opcode has multiple useful covers.

## 9.9 Verify rules offline

Use three levels:

1. Exhaustive small-domain testing for narrow integers and shifts.
2. Differential execution against current Railshot.
3. SMT verification for algebraic, width, and address transformations.

VeriISLE demonstrates the practical model: rule chains are checked offline using SMT and authoritative ISA semantics; no solver is needed during compilation.

---

# 10. Bounds checks and memory-address transformations need proof-carrying lowering

This should be treated as a security requirement, not just a testing detail.

In April 2026, Wasmtime disclosed:

- A Winch bug where upper bits in an address register were assumed clear, allowing out-of-sandbox memory access.
- A Cranelift AArch64 bug where the address used for checking diverged from the address used for the actual load.
- A Winch `table.size` width error that could expose host stack data.

Railshot’s selector should therefore never independently reconstruct “equivalent” checked and accessed addresses.

Use an explicit address identity:

```go
type MemAddrID uint32

type MemAccess struct {
    Addr        MemAddrID
    IndexWidth  MachineType
    AccessWidth uint8
    Offset      uint64
    Memory      uint32
}

type BoundsProof struct {
    Addr        MemAddrID
    AccessWidth uint8
    Kind        BoundsProofKind
}
```

The bounds check and the actual memory instruction must consume the same `MemAddrID`. A transformation that changes the address either:

- Transforms the proof and access together under a verified rule, or
- Invalidates the proof and emits a new check.

For declared-minimum elimination, only remove a check when all of these hold:

```text
same memory index
same effective-address expression
correct 32/64-bit wrapping semantics
offset + access width cannot overflow
effective range is below the declared minimum
memory cannot shrink
no selector rule later substitutes a wider load
```

The same width discipline applies to tables, multi-value ABI results, GC references, and vector memory instructions.

---

# 11. Compact-core migration strategy

Do not convert every Railshot feature simultaneously, and do not mix two compiler states halfway through a function.

## Whole-function eligibility

The initial compact path should accept functions containing:

- Scalar integer constants and arithmetic.
- Integer comparisons.
- Basic locals and globals.
- Scalar loads and stores.
- `select`.
- Basic blocks, loops, and `if`.
- Direct calls only after scalar call lowering is ready.

Initially exclude:

- SIMD.
- WasmGC.
- Exception handling.
- Atomics.
- Multi-memory and memory64.
- Typed function references.
- Plugin/custom instructions.
- Complex multi-result calls.

An unsupported function compiles entirely through current Railshot. There is no translation of partially built trees or allocator state.

This gives a clean safety boundary:

```text
function is compact-eligible
    → compact path from byte 0

function is not compact-eligible
    → current path from byte 0
```

## Never fall back by recompiling after emission

Feature admission must happen before compilation.

Resource-cap exhaustion inside the compact path must:

- Materialize pending state.
- Clear local optimizer facts.
- Continue with canonical scalar lowering.

It must not restart the function through the old backend.

## Shadow mode

In CI and benchmark builds:

1. Compile eligible functions with both paths.
2. Run differential inputs.
3. Compare traps, results, memory, tables, globals, and host effects.
4. Record native-code and compile-resource deltas.
5. Never expose dual compilation in production.

## Delete as coverage grows

The compact core must not become a permanent second implementation.

Set removal milestones:

```text
50% ordinary scalar function coverage
80% scalar corpus byte coverage
95% non-GC/non-SIMD corpus byte coverage
```

At each milestone, move shared semantics into the compact core and delete the corresponding old node/lowering machinery. The goal is convergence, not dual maintenance.

---

# 12. Recommended PR sequence

| PR | Change | Must remain identical | Primary gate |
|---:|---|---|---|
| 0 | Extend current stats with stage, scratch, scan, retry, and pressure metrics | Code and artifacts | Disabled overhead statistically zero |
| 1 | Split `funcHints` into fixed `FuncSummary` plus side tables | Native-code hash | Header ≤64 B |
| 2 | Reusable local/global scan scratch and exact top-K retention | Pin choices and code hash | Dense function × global matrix gone |
| 3 | Retry-cost capture and pressure estimator | Existing production policy | Predictor validated against observed pressure |
| 4 | Predictive pin budgeting | Semantics | Zero retries on corpus |
| 5 | Optional pin relinquishment | Semantics | No full retry on adversarial functions |
| 6 | Retained versus ephemeral arena chunks | Native-code hash | Giant-then-tiny retained memory collapses |
| 7 | Weighted memory scheduler and giant lane | Serial/parallel byte identity | Lower RSS at equal workers or faster at equal budget |
| 8 | Adaptive `RootNone` / conservative / exact plans | Root correctness | Lower root-analysis memory |
| 9 | Move cold `storage` fields to side tables | Native-code hash | Ordinary node no pointers from cold payload |
| 10 | Replace node links with `NodeID`; operand stack becomes IDs | Native-code hash | Pointer-free common backing |
| 11 | Bounded packet recycling/canonicalization | Semantics | Hard cap proven |
| 12 | Restricted pending locals and rematerialization | Semantics | Fewer local stores/reloads |
| 13 | Costed scalar tree covering | Semantics | Runtime win without compile/memory regression |
| 14 | Bounded call/merge resolver and expanded regions | Semantics | Fewer moves and call-local reloads |
| 15 | Tiny machine window | Semantics | Better code bytes/uops |
| 16 | Generated matcher plus offline verification | Semantics | Matcher ≤2% of scalar codegen time |
| 17 | Compact scalar path enabled by default | Public behavior | Passes all aggregate gates |
| 18 | Delete superseded old scalar machinery | Public behavior | Net source and binary complexity decrease |

PRs 1–2, 3–7, and 9–11 are the main memory/latency sequence. PRs 12–16 are the main generated-code-quality sequence.

---

# 13. Acceptance framework

## Correctness gates

Every PR touching lowering must pass:

- Official MVP, Release 2, and enabled Core 3 suites.
- Both explicit and signal-backed bounds configurations.
- AMD64 and ARM64.
- Malformed and invalid corpus.
- WasmGC and EH suites when the change can reach those paths.
- Differential execution against the previous path.
- Random instruction-sequence generation.
- Trap code and trap-order comparison.
- Host-call side-effect comparison.
- Serial/parallel deterministic artifact comparison.

For address, width, or selector changes, add:

- 32-bit and 64-bit address overflow cases.
- Maximum offsets.
- Narrow loads at the end of memory.
- Sign- and zero-extension combinations.
- Table32/table64 width differences.
- Multi-result upper-bit poisoning tests.
- Guard-page and explicit-check parity.

## Compile-latency gates

I would use these as default rejection thresholds:

| Metric | Gate |
|---|---:|
| Tiny-module p50 | No regression above 1.5% |
| Full-corpus compile geomean | No regression above 1.5% for memory-only changes |
| Large-module p50 | No regression above 2% |
| Large-module p99 | Must improve for retry/scheduler work |
| Metrics-disabled overhead | Below measurement noise |
| Matcher/window share | At most 2% of scalar backend time |
| Cap-exhaustion latency | Linear and bounded |

A code-quality change may spend compile latency only when its execution improvement passes a declared exchange rate. For example:

```text
+1% compile geomean allowed only for:
    ≥3% execution geomean improvement
    or ≥5% improvement in an explicitly targeted hot workload
```

## Memory gates

| Metric | Gate |
|---|---:|
| Summary header | ≤64 bytes/function |
| Common value node | ≤32 bytes |
| Common node backing | Pointer-free |
| Per-op allocations | Zero |
| Serial peak RSS | Never regress for memory-only changes |
| Parallel peak RSS | At least 20% lower at equal workers, or equivalent throughput gain at fixed budget |
| Post-giant retained scratch | Near ordinary-worker baseline |
| Root-analysis cap case | Correct fallback without unbounded growth |
| Optimizer packet | Hard 64-node limit |
| Machine window | Hard 16-op limit |

## Generated-code gates

| Metric | Gate |
|---|---:|
| Execution geomean | Positive and statistically supported |
| Individual regression | Investigate above 1.5%; reject above 3% without a clear trade |
| Native-code geomean | No more than 2% growth by default |
| Per-function growth | Must fit function and module byte budgets |
| Spills/reloads | Non-increasing on target workloads |
| Calls/merge moves | Non-increasing |
| Trap and safepoint metadata | Correct and bounded |
| Artifact determinism | Exact |

---

# 14. Work I would explicitly avoid

## Do not build whole-function SSA

The baseline compiler literature confirms the compile-time advantage of direct lowering, and Wago has already chosen Railshot as its sole execution backend. Liftoff and Winch validate this direction; TPDE shows that better local integration does not require splitting the compiler into global IR phases.

## Do not build an e-graph

An e-graph would add pointer-rich state, unpredictable saturation, large hash tables, and awkward trap/effect constraints. Offline superoptimization can discover rules; production should execute only bounded generated matchers.

## Do not add whole-function live intervals

The current problem is not a universal spill storm. Exact near-future use within a pending packet, local region information, and explicit call/merge handling offer most of the useful benefit with bounded memory.

## Do not start with `sync.Pool`

Pooling the current pointer-rich arena would retain high-water and make ownership less clear. First shrink the representation, make backing pointer-free, and define retention limits. Pool only small stable worker objects afterward.

## Do not revive a frontend rewrite

Production decoding is already byte-backed and avoids function-body instruction trees. The remaining frontend opportunity is to fuse compatible summaries and avoid repeated immediate decoding without disturbing validation error order.

## Do not retry direct parallel mmap assembly

That experiment already failed Wago’s balanced latency gate. Revisit only if a future code-image or page-population primitive materially changes the cost.

## Do not accumulate unranked peepholes

Wago already contains many strong exact peepholes. Every new pattern should come from one of:

- A hot disassembly.
- A high counter.
- An offline search result.
- A recurring missed cover in selector diagnostics.

Once a family has several overlapping cases, replace the handwritten cascade with a generated rule table.

## Do not expose dozens of resource knobs

Internally, Railshot may need separate limits for nodes, roots, facts, matcher fuel, machine window, and code growth. Publicly, expose at most a small policy seam such as:

```go
type CompileResources struct {
    MemoryBytes uint64
    Workers     int
}
```

Optimization internals should derive their caps from that policy.

---

# 15. Realistic target envelope

These are engineering targets, not promised results.

## After summary compaction, retry elimination, and worker lifecycle

Aim for:

- 10–20% lower full-compile allocated bytes on many-function modules.
- More substantial reductions on function/global-matrix stress cases.
- 15–25% lower parallel peak RSS at the same worker count.
- Materially lower p99 on register-hostile functions.
- No generated-code changes from the first several PRs.

These targets are consistent with current measured wins: the recent arena work already reduced `utf-as` backend allocation bytes by 18.3%, while function-result scratch reuse cut allocation counts by about 5.2% on Ruby and 5.4% on esbuild without changing native-code hashes.

## After pointer-free nodes

The common node payload target is an exact 112-to-32-byte reduction, or 71.4%. Total backend memory will fall by less because code, summaries, root structures, and side tables remain.

The more important secondary target is:

- A major decrease in `/gc/scan/heap`.
- Lower mark-assist CPU under parallel compilation.
- Better repeated-compile behavior after giant functions.
- Less allocator and cache traffic while walking deferred trees.

## After the quality lane

I would set:

- 5–10% execution geomean improvement across the currently losing compute kernels.
- 15% or better improvement on at least some targeted call-, merge-, or SIMD-heavy workloads.
- No broad compile-latency regression.
- No more than 2% native-code geomean growth.
- No regression in Wago’s strongest existing workloads.

The quality gains are most likely to come from:

1. Fewer call and merge moves.
2. Better child evaluation order.
3. Rematerialization instead of spill/reload.
4. Better memory and immediate folding chosen as a whole tree.
5. Bounded regional local allocation.
6. Exact machine-level cleanup.
7. Generated multi-operation SIMD/SWAR covers.

They are less likely to come from a generally “smarter” global spill allocator, because several known slow functions already report zero ordinary spills.

---

# Final recommendation

The first concrete implementation sequence should be:

```text
1. Extend existing stats with memory, retry, and pressure accounting.
2. Replace 200-byte funcHints with ≤64-byte FuncSummary records.
3. Reuse dense scan scratch and retain only exact top-K decisions.
4. Add Sethi–Ullman-style pressure estimates to the existing body scan.
5. Use those estimates to eliminate full unpinned recompilation.
6. Discard giant worker overflow chunks and add a byte-budget scheduler.
7. Make root analysis adaptive.
8. Move rare storage out of elem.
9. Convert elem pointers to NodeID and shrink the common node to 32 bytes.
10. Add restricted pending locals, rematerialization, and costed tree covering.
11. Add a 16-operation machine window and offline-generated matchers.
12. Delete old scalar machinery as compact-core coverage expands.
```

That sequence attacks the real current bottlenecks without undoing Railshot’s defining advantage. It preserves direct compilation, keeps optimizer state bounded, lowers Go GC involvement, eliminates repeated work, and raises machine-code quality through better **local integration** rather than a heavyweight global representation.
