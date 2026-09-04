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
- Compact pointer-rich `ctrlFrame` records before or alongside operand-node conversion. The implemented split now leaves ordinary frames at 72 bytes on both architectures; further control-state work should target measured sidecar high-water rather than restoring cold fields to the common record.
- Resolve module invariants once, narrow compiler-only indexes, flatten parallel metadata, and apply retention limits per scratch buffer.
- Retire default-off experiments and mature rollback switches that fail the normal qualification gates. Compiler mechanisms should replace old state, not accumulate beside it.

The implementation currently includes one hundred and thirty general cuts from that audit:

1. Module hint scanning always retains exact touched-global records instead of a dense function-by-global matrix, and the fixed hint record drops from 200 to 152 bytes. On a synthetic 1,024-function/1,024-global shape with one touched global per function, this changed the ARM64 hint benchmark from approximately 5.47 MB and 0.64 ms per operation to 0.24 MB and 0.12 ms per operation. This is a targeted stress result, not a full-corpus claim.
2. Module-wide synchronous-host-call classification is computed once per module, and the bounded module-global pin list replaces a per-function `globals`-sized membership bitmap.
3. The existing opt-in statistics path now exposes a shared compile-resource ledger for hint headers and sidecars, function attempts, worker scratch, and stage time. Timing is explicitly excluded from deterministic stats comparisons. Deterministic byte/count fields remain comparable; worker scratch retention fields intentionally report actual worker topology and scheduling and are excluded from serial/parallel artifact-stat comparisons. Failed-attempt accounting was retained while whole-function retry was being qualified and removed with that production path in item 120.
4. The first control-stack cuts move EH-only state behind a lazy semantic sidecar, group scalar fields to remove alignment holes, and stop allocating all-false GC-root vectors. Together they reduce every ordinary `ctrlFrame` from 472 to 408 bytes on AMD64 and from 416 to 368 bytes on ARM64. On a generated 128-deep scalar-block benchmark, AMD64 allocations fell from 283 to 155 per compile and allocated bytes moved from roughly 229.7 KiB to 209.8 KiB; median latency was effectively flat across the before/after local screens. This is an adversarial shape result, not completion of the 32–48-byte control-frame target.
5. The failed, default-off `fcmp-fuse` experiment is retired on both architectures. Its screening showed no aggregate execution win, slightly worse compile time when enabled, unchanged compile allocation, and focused wins below the generated-code acceptance gate. Removing the deferred-node path, public option, environment controls, schema entry, and experiment-only test deletes a net 283 backend source lines while retaining eager float-comparison semantics and coverage. Like the other early changes, default native code remains byte-identical; the production binary-size change is negligible (-160 bytes AMD64, +104 bytes ARM64 in matched Linux builds), so this is claimed only as a source-complexity reduction.
6. The default-off AMD64 `call-next-use` experiment is also retired. Its bounded post-call bytecode rescan and alternate spill policy bought only 0.10% aggregate execution, with a worst focused result of 0.84%, while disabling it improved compile time by 0.25% and left compile allocation unchanged. The removal deletes a net 168 AMD64 backend lines and two per-function masks; matched Linux/AMD64 artifacts for recursive-call, many-function, and float fixtures remain byte-identical. Calls now have one general dirty-local spill policy rather than a dormant alternate path.
7. Immutable-table proofs are now retained once per module instead of copied into every function summary. This removes one slice header from AMD64 summaries and four module-wide scalar fields from ARM64 summaries, reducing `funcHints` from 152 to 128 bytes on both architectures. The `many_funcs` full-compile benchmark falls from 220,625 to 212,433 B/op (-3.7%) with allocation count unchanged; an eight-sample local screen showed no latency regression. Matched many-function, float, and indirect-dispatch artifacts remain byte-identical.
8. The reusable dense global-hint accumulator is now explicit scan scratch rather than a pointer field retained in every finished summary. This reduces `funcHints` from 128 to 120 bytes and removes one scanned pointer per function. The 1,024-function sparse-global stress benchmark falls from 216,280 to 208,088 B/op (-3.8%) with the same 24 allocations and unchanged steady-state latency; the smaller `many_funcs` allocation remains in the same Go size class. Matched many-function, global-heavy, and indirect-dispatch artifacts remain byte-identical.
9. Retained local-score, last-use, and sparse-global slice headers are replaced by checked 32-bit ranges into module-owned sidecars. Stack-local views reconstruct the slices only while scanning or compiling a function, and the compile boundary passes that larger view by pointer to avoid a Go ABI copy cliff. This reduces `funcHints` from 120 to 64 bytes on both architectures (-46.7%) and removes the temporary per-function sparse-range array. On ARM64, the 1,024-function sparse-global stress benchmark falls from 208,088 to 134,168 B/op (-35.5%) and from 24 to 21 allocations; `many_funcs` falls from 150,776 to 125,240 B/op (-16.9%) and from 42 to 39 allocations. Five-sample default-GC medians moved from 216.54 to 217.38 microseconds (+0.4%); with GC disabled they moved from 217.12 to 214.31 microseconds (-1.3%). Matched `many_funcs`, `globals`, `dispatch`, and `json-as` native-code hashes remain identical.
10. ARM64 module hint construction now stores its validated local count directly in the compact summary instead of retaining a duplicate host-width `[]int`. The 1,024-function stress benchmark falls again from 134,168 to 125,976 B/op and from 21 to 20 allocations; `many_funcs` falls from 125,240 to 122,552 B/op and from 39 to 38 allocations. This is the same general per-function metadata deduplication used by AMD64, not a corpus-selected path.
11. ARM64 whole-function pins now leave four unreserved registers for the widest ordinary lowering step: three simultaneously protected inputs or temporaries plus one result. The limit is derived from the target register file after module roles are removed; it never consults module identity, function identity, producer metadata, or corpus membership. This removed the two remaining retries in the complete benchmark corpus before the retry path itself was deleted in item 119. On Ruby, attempts fell from 17,454 to 17,452, eliminating 4,299 re-read input bytes, 109,648 failed-attempt node bytes, and 4,060 discarded code bytes. Unlike the old all-or-nothing retry, the admitted first attempt retains 16 profitable pins in each affected function and shrinks the full native image by 2,592 bytes. Five one-shot full-compile medians moved from 608.95 to 610.21 milliseconds (+0.2%).
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
81. AMD64 single-function GC-resolver selection no longer recompiles a completed function. The function starts with the inline resolver and, only when lowering reaches a second actual resolution after bounded resolved-address reuse, switches all subsequent sites to the existing shared resolver. Repeated accesses that collapse to one resolution retain byte-identical 451-byte code; an eight-distinct-object fixture uses one inline resolution plus seven shared calls in one attempt, producing 1,076 bytes versus 948 for the former two-attempt result and 1,798 fully inline. Twelve interleaved native Ryzen 7 7800X3D pairs reduce compile time from 34.38 to 21.79 microseconds (-36.64%), allocation from 30.52 to 28.97 KiB/op (-5.09%), and allocations from 87 to 75 (-13.79%). The focused +128-byte code-size cost is accepted because it removes a whole-function retry while remaining roughly 40% smaller than fully inline code. Twelve pinned 500 ms execution pairs on the repeated-access fixture remain statistically unchanged at 430.4 versus 435.8 ns/op (p=0.124), with zero allocations. Selection depends only on candidate count, bounded reuse, and actual semantic resolution events; it never inspects producer, corpus, module, or function identity.
82. AMD64's default-off exact GC-reference-fact compiler is retired rather than retained as a workload-selectable alternate path. Its broad qualification was execution-neutral (-0.02%), while disabling it improved compile time by 4.10%, compile allocation bytes by 3.83%, and compile allocation count by 11.66%; the focused MoonBit WasmGC result was only 1.38%. The public option, environment opt-in, manifest/schema name, inline zero-fact sidecar, serial local-fact reservation, and experiment-specific differential tests are removed. Required GC roots and the independent bounded resolved-address cache remain. Default native code is unchanged because the experiment was already off. This reverses the provisional retention described in items 75–77: a focused workload win no longer justifies a permanent second compiler state.
83. The now-unreachable AMD64 fact engine is physically removed instead of being left as dead optimizer infrastructure. Its 967-line implementation is split into a 90-line production resolved-address cache and small canonical-lowering shims while call sites are collapsed; per-function local-fact, constructor-identity, array-length, and struct-field state is deleted. Across the two removal commits, a matched stripped Linux/AMD64 runtime example falls from 6,426,776 to 6,353,048 bytes (-73,728, -1.15%). Five interleaved 200 ms `GOGC=off` pairs across sixteen scalar, control, SIMD, and branch-table compile fixtures improve time by 6.83% geomean and B/op by 0.36%, with allocation count unchanged; fifteen rows improve and `SIMDMixedResults` moves +1.35%. All 64 decodable modules in the native AMD64 corpus retain identical code lengths and SHA-256 hashes. The retained resolved-address cache uses only local provenance and semantic invalidation boundaries, never exact reference facts or workload identity.
84. AMD64 structured control no longer carries the retired fact engine through compatibility sidecars. Five fact-vector headers per fact-bearing frame, two transient fact slices, the fact-sidecar scratch arena and ledger branches, block-parameter fact construction, and merge/install calls are deleted; the independent root-only sidecar remains the sole structured-control GC state. This follow-up removes 321 net source lines and reduces the same matched stripped Linux/AMD64 runtime example from 6,353,048 to 6,332,568 bytes (-20,480), for a cumulative 94,208-byte (-1.47%) reduction from the pre-retirement baseline. Native AMD64 backend, shared-compiler, optimization, and focused GC runtime tests pass, and all 64 decodable corpus modules retain identical native-code lengths and SHA-256 hashes. The change removes unreachable compiler state uniformly; it has no new selector or workload-specific path.
85. The final AMD64 fact-retirement pass removes the 105-line compatibility shim, its dead nullability/type/length folds, load-forwarding and known-bounds switches, and the backend-neutral 288-line fact representation. The shared package retains only the 17-line barrier-state contract used by native store telemetry; GC references keep their independent root bit, reference stores fail closed to the conservative barrier, and the one-entry resolved-address certificate remains. The matched stripped Linux/AMD64 runtime example falls another 8,192 bytes to 6,324,376, a cumulative 102,400-byte (-1.59%) reduction from the pre-retirement baseline. Native AMD64 backend, shared-compiler, optimization, and focused GC runtime tests pass, and all 64 decodable corpus modules retain identical native-code lengths and SHA-256 hashes. Current GC and optimization documentation no longer advertises removed switches; historical measurements remain explicitly labeled. This deletes one dormant compiler design rather than replacing it with a shape- or workload-selected path.
86. AMD64 no longer performs a second full bytecode walk at every loop header solely to populate the diagnostic `BoundsChecksHoistable` counter. GC facts were the scan's last code-generation consumer; after their retirement, actual bounds certificates and emitted checks did not read the modified-local set. The classifier walk, sort/deduplicate buffer, per-function scratch slices, control-frame range, and scan-only tests are deleted, while ordinary in-loop and bounds-check counters remain. Eight interleaved native Ryzen 7 7800X3D pairs on the SIMD loop-parameter compile fixture improve median time from 27.89 to 25.62 microseconds (-8.15%), B/op from about 29.52 to 27.96 KiB (-5.27%), and allocations from 61 to 60. A matched stripped Linux/AMD64 runtime example falls another 16,384 bytes to 6,307,992. Native AMD64 backend and focused GC runtime tests pass, and all 64 decodable corpus modules retain identical native-code lengths and SHA-256 hashes. This removes 198 net source lines and one redundant pass for every loop, with no replacement selector or workload-specific admission.
87. The GC-fact retirement also makes two AMD64 no-barrier reference-store emitters, the no-barrier array-fill helper dispatch, four speculative barrier-state telemetry values, and their shared contract tests unreachable. They are deleted instead of being preserved as an unselectable future path. Reference stores continue to use the conservative barrier; pointer-free array fills record the direct no-barrier fact without a second policy enum. This removes 131 net code lines. Native AMD64 backend, shared-compiler, runtime-GC, and focused GC product tests pass, all 64 decodable corpus modules retain identical native-code hashes, and the matched stripped binary is unchanged because the Go linker already discarded the unreachable functions. This is a source-complexity cut, not a claimed runtime-memory win.
88. The workload-independence policy is now executable rather than only documented. A production-source AST audit rejects benchmark/corpus identifiers in string literals, module/function name-section reads outside the diagnostic naming files, hashing imports, and byte-prefix/suffix/containment matchers anywhere in the Railshot backends. Instruction-shape recognizers remain valid because they decode typed operations and immediates; exact native-byte equality remains valid for post-generation deduplication. The audit runs without compiling the opposite architecture and therefore covers AMD64, ARM64, and shared production sources from either host. This changes no production code or generated bytes; it prevents a future corpus-, producer-, name-, hash-, or memorized-body selector from silently entering the compiler.
89. Three mature AMD64 native-compaction rollback switches are retired: `WAGO_AMD64_NO_INCDEC`, `WAGO_AMD64_NO_DIRECT_INCDEC`, and `WAGO_AMD64_NO_DIRECT_JECXZ`. They selected no semantic fallback and only restored the older, longer encoding after the compact `INC`/`DEC` and bounded `JECXZ` forms had passed their qualification suites. Compact selection now depends solely on the existing `CompactNative` objective plus the same flag-liveness, register, immediate, and branch-range proofs; ordinary Speed/Balanced lowering remains unchanged. Compact and ordinary tests retain direct result, encoding, counter, and bulk-tail coverage without mutating package-global policy. This removes three environment reads and their alternate production branches uniformly, reducing production Railshot's distinct `WAGO_*` controls from 121 to 118 without adding any workload selector.
90. AMD64 constant-pool preparation no longer has an ungated test-only mode that rescans every function body for both scalar-float and vector constants. Production had always enabled the existing one-pass hint gates, so the mutable package global and four alternate branch arms were unreachable outside a differential test. The compiler now runs each preload scan only when the semantic hint records that instruction family; direct integer, scalar-float, and SIMD tests retain exact scan-count coverage. All 64 decodable AMD64 corpus modules retain identical native-code hashes. This deletes a dormant repeated-body-walk policy rather than adding a body-, function-, or workload-specific exception.
91. ARM64's constant-cache prepass now runs only when the existing function-summary scan observes an `f32.const` or `f64.const`. The new semantic flag occupies spare bits in the already-32-byte `funcHints` record and is populated by both byte-backed and programmatic decoded-body scanners; there is no new allocation, pass, or workload gate. Six interleaved native Apple M4 Max pairs of the integer-heavy `many_funcs` full compiler reduce time by 3.69% geomean across ordinary and compact modes, from 205.9 to 198.3 microseconds, with the same 73.34 KiB/op and 31 allocations. All 64 decodable ARM64 corpus modules retain identical code lengths and hashes. Float-containing functions retain the same bounded prepass and generated code; functions without float constants now avoid a redundant full body walk.
92. AMD64 range bounds certificates no longer have a second package-global rollback switch. `WAGO_NO_BOUNDS_RANGE` only disabled the fixed-offset range extension inside the already-policy-controlled bounds-fact engine; the public `bounds-facts` selection and `CompileOptions.NoBoundsFacts` remain the exact per-compilation differential and safety oracle. Removing the redundant environment read leaves one general policy boundary while preserving the existing shared-memory, guard-mode, overflow, alias, and invalidation checks. Default code is unchanged, and focused tests still compile the same access sequence with facts enabled and disabled. Production Railshot's distinct `WAGO_*` controls fall from 118 to 117 without removing a correctness fallback or introducing workload selection.
93. AMD64 compact i32 frames no longer carry separate control-flow and call-site rollback globals. `WAGO_AMD64_NO_COMPACT_I32_CONTROL` and `WAGO_AMD64_NO_COMPACT_I32_CALLS` only restored subsets of the older unpacked layout; disabling the public `compact-i32-frame` option already restores that entire representation per compilation. The call staging helper now always loads a local at its validated machine width, and compact-frame admission retains its EH, exact-GC-root, and minimum-local exclusions. Focused straight-line, structured-control, and non-inlined-call tests now compare the public policy states without mutating package globals and execute the packed results. Default native code is unchanged. Production Railshot's distinct `WAGO_*` controls fall from 117 to 115, consolidating three partial policies into one general representation choice.
94. ARM64 zero-branch selection no longer carries separate empty-edge and concrete-`eqz` rollback globals. `WAGO_ARM64_NO_EMPTY_ZERO_BRANCH` and `WAGO_ARM64_NO_EQZ_ZERO_BRANCH` only disabled subsets of the already-policy-controlled lowering; the public `zero-branch` option restores the complete `CMP+B.cond` representation per compilation. Empty-edge admission remains restricted to compact native policy after proving reconciliation emitted no bytes, while concrete `eqz` admission still requires a condensable register operand. Focused zero, nonzero, high-bit, `if`, `br_if`, and concrete-`eqz` tests retain execution and exact code-size coverage through the public policy. Default native code is unchanged. Production Railshot's distinct `WAGO_*` controls fall from 115 to 113 without changing semantic admission or introducing workload selection.
95. Cross-function trap-body sharing no longer carries a second module-only rollback on either architecture. `WAGO_ARM64_NO_MODULE_SHARED_TRAP_BODY` and `WAGO_AMD64_NO_MODULE_SHARED_TRAP_BODY` preserved intermediate per-function layouts beneath the already-policy-controlled optimization; disabling the public `shared-trap-body` option restores the complete pre-full-body-sharing layout per compilation. ARM64 also deletes its body-before-groups emitter, which existed solely to reproduce that intermediate checkpoint. Serial and parallel focused tests now exercise both public policy states, require zero versus one cross-function share, verify the native-size ledger, and execute every trap arm. Default native code is unchanged. Production Railshot's distinct `WAGO_*` controls fall from 113 to 111, removing an alternate cold-code representation without weakening range, PC-relative, literal-island, host-adapter-boundary, or exact-body admission checks.
96. ARM64 function-local trap-unwind sharing no longer retains `WAGO_ARM64_NO_SHARED_TRAP_UNWIND`, a qualification-only rollback for the mature compact-code layout. Compact functions with at least two trap groups still share one terminal unwind; ordinary policy retains local tails, and an out-of-range branch still falls back to a local unwind. The existing focused test disables complete trap-body sharing through its public policy and verifies the independent local-tail saving and execution, so removing the environment branch does not weaken semantic coverage. Default native code is unchanged. Production Railshot's distinct `WAGO_*` controls fall from 111 to 110 without adding a new selector or changing the instruction-count and branch-range admission rules.
97. ARM64 compiler-authored zero/nonzero tests now have one direct `CBZ`/`CBNZ` lowering. `WAGO_ARM64_NO_DIRECT_ZERO_BRANCH` and the alternate `CMP+B.cond` helper branches existed only as the accepted optimization's qualification oracle; callers already establish that flags are dead, while trap sites retain their exact bounds counter and shared-stub metadata. The focused encoder test covers 32- and 64-bit `CBZ` and `CBNZ`, requires one four-byte instruction, and the broader table, call, reference, GC, EH, memory64, interrupt, and trap suites retain execution coverage. Default native code is unchanged. Production Railshot's distinct `WAGO_*` controls fall from 110 to 109, deleting a mature alternate lowering without any module, function, producer, or corpus selector.
98. Call-free register-only void leaves no longer carry separate result-shape rollback globals. Both architectures use the same already-proved frame-touch exclusions for void and single-register results; AMD64's public `frame-elide` option remains the complete per-compilation oracle, while ARM64 retains its ordinary-policy and register-homing gates. The AMD64 test now compares and executes the public policy states, and the ARM64 test directly requires the accepted zero-frame layout and execution without mutating a package-global subset switch. Default native code is unchanged. Removing `WAGO_AMD64_NO_FRAME_ELIDE_VOID` and `WAGO_ARM64_NO_FRAME_ELIDE_VOID` reduces production Railshot's distinct `WAGO_*` controls from 109 to 107 without changing call, spill, EH, vector-local, extended-inline-local, or unpinned-local exclusions.
99. Register-ABI internal frames no longer carry process-global switches that restore the wrapper-sized header. Both backends now select the compact header directly from ABI and semantic compatibility: wrapper entries retain their header, EH and incompatible exact-GC-root plans fail closed, and AMD64 tail-call functions keep the results-pointer layout required by wrapper transfer. Focused tests require the accepted frame-header event, execute the resulting code, and retain direct GC-root offset-remapping and rejection coverage without constructing a package-global rollback state. Removing `WAGO_AMD64_NO_COMPACT_REGABI_FRAME` and `WAGO_ARM64_NO_COMPACT_REGABI_FRAME` reduces production Railshot's distinct `WAGO_*` controls from 107 to 105. Default native code is unchanged, and there is no module-, function-, producer-, or corpus-dependent admission beyond the stated semantic constraints.
100. Control frames now use one kind-discriminated native-code site for a loop's backward target or an `if` frame's false edge, reducing the common `ctrlFrame` from 88 to 80 bytes (-9.1%) on both architectures. The apparently spare field had also held a non-loop frame's second forward-end patch site; that ownership moves into existing cold merge storage instead. AMD64 fills four bytes of padding in its unchanged 88-byte merge record, while ARM64 uses the unchanged four-byte word that stores loop-only modified-local count/validity as the non-loop second site; its merge record remains 136 bytes. A native AMD64 128-deep scalar-control screen reduces allocation by about 256 B/op with the same 90 allocations and moves time by -0.30% geomean across eight corresponding `GOGC=off` samples; timing is directional because the samples were collected in sequential baseline/candidate batches. All 64 decodable modules retain identical native-code hashes on each architecture. Focused tests pin both sidecar sizes and prove that two inline forward ends do not overwrite a live `if` false edge. The representation applies to every control frame and is selected only by validated control kind.
101. Control frames now store branch arity in its exact Wasm `u32` domain and pack the reusable base-type arena range into one 16-bit word. The hot arena is statically bounded to 64 entries, while cold multi-value frames derive their exact base count from the already-retained combined base/parameter/result type slice, so wide validated signatures neither truncate nor add an indirection. Field ordering then reduces the common `ctrlFrame` from 80 to 72 bytes (-10.0%) on both architectures without changing either cold sidecar. On the native AMD64 128-deep scalar-control screen, eight sequential `GOGC=off` samples reduce allocation from 82,253.9 to 79,452.8 B/op (-3.4%) with the same 90 allocations; time moved by -4.2% geomean, which is directional rather than claimed because the batches were not interleaved. All 64 decodable corpus modules retain identical native-code lengths and hashes on AMD64 and ARM64. Focused cold-arena, wide-signature, control, and corpus tests cover both representations. This is unconditional exact-domain packing and contains no workload selector.
102. Exact-GC control frames now retain one contiguous base/parameter/result root vector instead of three independent slice headers and up to three backing arrays. The segment offsets are already exact in each frame's stack height and parameter/result arities; three presence bits fit in the existing control flag word, preserving the prior `nil` representation for all-false segments without enlarging the common frame. This reduces `ctrlFrameRoots` from 72 to 24 bytes (-66.7%) on both architectures. A generated native AMD64 function carrying a hidden reference through 128 nested blocks and an allocating safepoint falls from 197,256 to 184,200 B/op (-6.6%) with the same 483 allocations; eight sequential `GOGC=off` samples move time by -4.2% geomean, which is directional rather than claimed. All 64 decodable corpus modules retain identical native-code lengths and hashes on both architectures, and focused tests prove shared segment ownership, capture of simultaneous base and parameter roots, result merging, sidecar reuse, and release. The layout depends only on exact control-frame arities and root state and has no workload selector.
103. The lazy exception-control record now stores its native handler site in the compiler's existing 32-bit code-offset domain and its handler-record index in the statically bounded four-entry domain. Placing those scalars and the three result-root flags after the catch slice reduces `ctrlFrameEH` from 48 to 32 bytes (-33.3%) on both architectures without changing the catch table or common control frame. Ten alternating native AMD64 `GOGC=off` pairs on the generated general exception-handling module reduce compile allocation from 47,024.8 to 46,865.2 B/op (-0.34%) with the same 198 allocations and move time by -0.49% geomean. All 64 decodable corpus modules retain identical native-code lengths and hashes on both architectures. The representation follows fixed EH resource limits and native-offset ownership only; it has no workload selector.
104. Exception catch clauses now encode their target frame and native match site in the compiler's existing 32-bit index/offset domains, and encode payload counts and the bounded root-record index in bytes. Field ordering then reduces each retained `ehCatchClause` from 56 to 20 bytes (-64.3%) on both architectures. Ten alternating native AMD64 `GOGC=off` pairs on the generated general exception-handling module reduce compile allocation from 46,865.1 to 46,257.2 B/op (-1.30%) with the same 198 allocations; time moves by +0.32%, inside the 1.5% memory-change gate. All 64 decodable corpus modules retain identical native-code lengths and hashes on both architectures. The packing follows validated branch-label, payload-arity, EH-root, and native-offset domains only and has no workload selector.
105. Parallel function handoff now retains one pair of adapter offsets instead of embedding separate adapter-tail and shared-adapter records. Those modes are mutually exclusive under the module's already-selected native-compaction policy, and the source function index is restored from the ordered result slot during the deterministic join. This reduces `funcResult` from 72 to 48 bytes (-33.3%) on ARM64 and from 80 to 56 bytes (-30.0%) on AMD64. Ten alternating native AMD64 `GOGC=off` pairs compiling the 301-function `many_funcs` module with four workers reduce allocation from 205,638.3 to 199,549.2 B/op (-2.96%) and time from 213.1 to 210.2 microseconds/op (-1.39%); allocation count is effectively unchanged. Serial compilation does not allocate this handoff table. Parallel shared-adapter, adapter-tail, relocation, stats, and corpus-parity tests preserve deterministic output. The representation applies to every parallel function and contains no module or workload selector.
106. Exact-GC safepoint plans no longer retain an explicit ID in every record. Valid plans already assign IDs densely as `SafepointBase + index + 1`; validation now proves that derived range, and the final runtime map reconstructs the same explicit IDs before compilation metadata is released. This reduces `GCFrameSafepointPlan` from 32 to 24 bytes (-25.0%) on 64-bit hosts. Ten alternating native AMD64 `GOGC=off` pairs compiling a generated exact-root function with 2,050 allocating safepoints reduce allocation from 12,799,094.2 to 12,763,603.5 B/op (-0.28%) and time from 6.060 to 6.030 ms/op (-0.50%); allocation count is effectively unchanged. Exact-root admission, dense lookup, round-trip, wide-root, and both backend suites preserve behavior. The representation applies to every exact-GC safepoint and contains no module or workload selector.
107. Finalizer data-fragment boundaries now use the compiler's existing 32-bit native function-offset domain instead of host-width integers. This reduces both ARM64 `finalizerFragment` and AMD64 `jumpTableFragment` from 24 to 12 bytes (-50.0%); explicit overflow flags preserve fail-closed errors instead of truncating an oversized function. Ten alternating native AMD64 `GOGC=off` pairs on the generated large mixed-`br_table` compiler benchmark reduce allocation from 52,027.2 to 51,849.0 B/op (-0.34%) with the same 29 allocations and time from 335.7 to 330.6 microseconds/op (-1.51%). All 64 decodable corpus modules retain identical native-code lengths and hashes on both architectures. The packing applies to every finalizer fragment and contains no module or workload selector.
108. The default-off ARM64 loop-region pinning experiment has been retired after its corpus qualification remained negative. Its environment switch, optimization-catalog and schema entry, alternate two-register allocator, branch-edge stores, scratch ownership, and dedicated tests are gone. The loop scan remains only for the default bounds-hoisting proof and now records just the local writes that proof consumes instead of also classifying calls, nested loops, `br_table`, and memory growth for the deleted allocator. Two otherwise-unused default-off environment parsers and four unused catalog constructors were removed with it, for a net deletion of 233 source lines. Sixteen alternating native ARM64 `GOGC=off` pairs on the forward-merge benchmark keep allocation exactly flat at 13.49 KiB/op and 27 allocations; time changes by a statistically insignificant +0.29% geomean. All 64 decodable corpus modules retain identical default native-code lengths and hashes. This removes an unqualified alternate path rather than adding another selector.
109. Exact-GC allocating-safepoint offsets now stream into one checked pointer-free function arena instead of retaining one 24-byte slice header and one backing allocation per site. Each site adds only a 4-byte count word followed by its offsets; a scoped builder prevents incomplete records from becoming visible, and final validation rejects truncated or trailing data. The fixed `GCFrameRootPlan` grows from 208 to 216 bytes, but a one-safepoint native AMD64 control still falls from 104 to 103 allocations with statistically unchanged time and 0.03% lower bytes. Twelve alternating native AMD64 `GOGC=off` pairs on a generated 2,050-safepoint exact-root function reduce compile allocation from 12.17 to 2.56 MiB/op (-78.97%), allocations from 7,372 to 5,324 (-27.79%), and time from 5.880 to 3.366 ms/op (-42.76%). All 64 decodable corpus modules retain identical native-code lengths and hashes on both architectures. The representation applies to every exact-GC allocating safepoint and contains no module or workload selector.
110. Exact-GC native callsites now use a second checked pointer-free stream instead of retaining a 32-byte record and independent offsets allocation for every native return path. Each stream record contains the return offset, stack adjustment, root count, and root offsets; finalization mutates return offsets through bounded views, and malformed or underflowing remaps fail exact-root admission. The stream count occupies existing `GCFrameRootPlan` padding, so the plan remains 216 bytes, while one reusable worker buffer replaces per-logical-call temporary allocation. A one-callsite native AMD64 control keeps the same 142 allocations and statistically unchanged time while reducing bytes by 0.02%. Twelve alternating native AMD64 `GOGC=off` pairs on a generated 2,048-callsite exact-root function reduce compile allocation from 709.0 to 650.3 KiB/op (-8.28%), allocations from 4,265 to 2,221 (-47.92%), and time from 926.2 to 889.2 microseconds/op (-4.00%). All 64 decodable corpus modules retain identical native-code lengths and hashes on both architectures. The representation applies to every exact-GC native callsite and contains no module or workload selector.
111. ARM64 single-bit conditional-branch folding no longer has a private process-global rollback. `WAGO_ARM64_NO_SINGLE_BIT_BRANCH` only suppressed candidate recording for the accepted `TST+B.cond` to `TBZ/TBNZ` rewrite, while the public `branch-fold` policy already disables that rewrite and all sibling branch folds per compilation. Candidate recording now consults the same immutable policy, so disabling branch folding also avoids writing unused candidate state. The finalizer still requires the exact one-bit mask, adjacent condition branch, untargeted middle instruction, and signed `imm14` range before rewriting. Default code is unchanged across all 64 decodable corpus modules, focused policy coverage checks both states, and production Railshot's distinct `WAGO_*` controls fall from 105 to 104 without a workload selector.
112. Exact-GC collector locals now retain their validated Wasm index and mutable native-frame offset together in one pointer-free `{Index, Offset uint32}` array instead of two parallel `[]uint32` arrays. This removes one slice header from every `GCFrameRootPlan`, shrinking it from 216 to 192 bytes (-11.1%), and removes both one initial and one compacted backing allocation per collecting function while preserving the same eight payload bytes per retained local. Ten `GOGC=off` planning samples over a generated 1,024-function module with one live collector local and safepoint per function reduce allocation from 545.3 to 505.3 KiB/op (-7.34%), allocations from 8,194 to 6,146 (-24.99%), and time from 399.2 to 373.2 microseconds/op (-6.51%) on native ARM64. Native AMD64 reduces allocation from 561.3 to 521.3 KiB/op (-7.13%), allocations from 10,242 to 8,194 (-20.00%), and time from 710.9 to 682.1 microseconds/op (-4.05%). Liveness, compact-frame remapping, local materialization, runtime validation, and root serialization consume the same ordered record, so mismatched parallel arrays are no longer representable. The layout applies to every exact-GC candidate and contains no module or workload selector.
113. Exact-GC local liveness now retains one pointer-free, site-major mask arena instead of separate allocation-site low words, call-site low words, and wide-mask extra words. Two checked 32-bit site counts replace two slice headers, shrinking `GCFrameRootPlan` from 192 to 152 bytes (-20.8%); narrow and wide functions allocate one final mask backing, while wide compaction creates a replacement arena only when its root mapping changes. Ten `GOGC=off` planning samples over a generated 1,024-function module with one live collector local, one allocating site, and one native call per function reduce ARM64 allocation from 425.3 to 393.3 KiB/op (-7.52%) and allocations from 6,146 to 5,122 (-16.66%), with time statistically unchanged at 326.3 versus 325.8 microseconds/op. Native AMD64 reduces allocation from 473.3 to 441.3 KiB/op (-6.76%), allocations from 9,218 to 8,194 (-11.11%), and time by 0.88%. A generated 64-to-1,024-root liveness matrix removes 14.3-25.0% of analysis allocations with unchanged allocation bytes and statistically flat time when sampled in baseline-then-candidate order. All 64 decodable corpus modules retain identical native-code lengths and hashes on both architectures. The arena applies to every exact-GC candidate and is indexed only by semantic safepoint kind and ordered site number; it contains no workload selector.
114. GC-frame planning now consumes validated, function-sorted exception root maps with a single cursor instead of first expanding them into a `[][]uint32` indexed across every local function. Only the collector-owned offsets for the current mapped function are materialized, with exact capacity, and the cursor still rejects an out-of-range trailing owner. Ten `GOGC=off` samples over a generated 1,024-function module with one exception root map reduce allocation from 67.93 to 41.30 KiB/op (-39.19%) and allocations from 5 to 4 (-20.00%) on both architectures. Planning time falls from 15.01 to 14.48 microseconds/op (-3.48%) on ARM64 and from 28.70 to 26.80 microseconds/op (-6.61%) on native AMD64. The stream follows the semantic function order already enforced by root-map validation and contains no function-shape, identity, producer, or corpus selector.
115. Module GC-frame ownership now uses a pointer-free dense `uint32` lookup into one contiguous sparse plan arena instead of retaining a scanned pointer for every local function and heap-allocating every collecting plan independently. Successful plan storage and failure diagnostics are separate return state, keeping the module record at 48 bytes, and each plan is built directly in its exactly reserved final arena. The existing semantic may-collect scan moves ahead of detailed planning and records its decision in the final lookup itself, allowing exact capacity without another body walk or temporary bitmap; admission fails closed if a marked function remains unpopulated. On a generated 1,024-function RootNone-heavy module, ten `GOGC=off` samples reduce ARM64 allocation from 9.523 to 4.273 KiB/op (-55.13%) with unchanged allocation count and statistically flat time, while native AMD64 falls from 9.547 to 4.297 KiB/op (-54.99%) with unchanged allocation count and time lower by 10.48%. When all 1,024 functions collect, ARM64 falls from 393.3 to 380.0 KiB/op (-3.37%), from 5,122 to 4,099 allocations (-19.97%), and by 4.44% in time; native AMD64 falls from 441.3 to 428.0 KiB/op (-3.00%), from 8,194 to 7,171 allocations (-12.48%), and by 6.53% in time. In ten alternating complete one-function compile pairs, RootNone and collecting time are statistically unchanged on both targets: ARM64 moves from 8.372 to 8.256 and from 10.64 to 10.54 microseconds/op, while native AMD64 moves from 17.33 to 17.30 and from 22.43 to 22.30 microseconds/op. RootNone bytes and allocation counts are identical; collecting bytes fall by 0.02-0.03% with unchanged allocation counts. The isolated ARM64 collecting-plan stage moves from 349.4 to 362.4 nanoseconds/op (+3.72%), which remains visible here even though both complete tiny-compile gates pass. All 64 decodable corpus modules retain identical native-code lengths and hashes on both architectures. Lookup and reservation depend only on decoded collection effects and source function order; they contain no workload selector. This is exact-plan ownership compaction; adaptive conservative root planning remains separate follow-up work.
116. Conservative EH roots now own the prefix of each plan's existing pointer-free safepoint arena instead of retaining an independent slice header in every collecting function. A checked 32-bit prefix count replaces the 24-byte header, shrinking `GCFrameRootPlan` from 152 to 136 bytes (-10.5%); reset preserves the prefix, safepoint iteration starts after it, malformed private bounds fail exact-root admission, and the fixed-root view is capacity-clipped so callers cannot overwrite an adjacent record. Ten alternating `GOGC=off` samples over the generated 1,024-function collecting module reduce ARM64 planning allocation from 380.0 to 364.0 KiB/op (-4.21%) and time by 3.03%, with the same 4,099 allocations. Native AMD64 allocation falls from 428.0 to 412.0 KiB/op (-3.74%) with the same 7,171 allocations and statistically unchanged time. Complete one-function RootNone and collecting compile pairs remain statistically unchanged on both targets; bytes and allocation counts are identical for RootNone, while collecting bytes fall by 0.05% with unchanged allocation counts. The fixed-root EH compile fixture is also statistically flat at 39.64 versus 40.04 microseconds/op across ten ARM64 pairs and 74.69 versus 74.78 microseconds/op across 25 balanced native AMD64 pairs, with unchanged bytes and allocations. Focused exact-root and EH tests pass natively on both architectures, and all 64 decodable corpus modules retain identical native-code lengths and hashes. The layout applies to every collecting plan and is independent of whether fixed roots are present; it contains no function-shape, identity, producer, or workload selector.
117. Compact function-relative metadata now fails through the ordinary backend error path instead of panicking when a valid but exceptionally large function exceeds a narrowed return-chain, control-end, or call-relocation domain. Both backends retain the first overflow as a one-byte per-function state, stop publishing the invalid compact value, and return a field-specific error when the current function or inlined body finishes; finalizer remapping also validates before narrowing. Ordinary lowering adds no per-opcode check, native code is unchanged, and focused boundary tests cover all three domains. Five alternating native AMD64 `GOGC=off` pairs across complete single-function RootNone, single-function collecting, and 1,024-function RootNone compiles keep bytes and allocations identical and move compile-time geomean by an unresolved +0.08%. The limit is purely representational and contains no workload selector.
118. Worker scratch accounting now merges through one architecture-neutral `CompileResourceStats.AddWorkerScratch` operation instead of duplicating the same eight-field accumulation in both backends. This removes a telemetry drift point without changing the ledger layout, opt-in behavior, or generated code; a shared unit test pins the summed semantics.
119. Whole-function register-exhaustion retry is removed from both production backends. AMD64 can now relinquish safe optional floating-point and vector local pins in place, matching its existing integer-local escape hatch: the local is homed, the register is lent to the current lowering step, and any temporary owner is evicted before the local is restored. Functions with more than 64 validated locals begin with optional whole-function and interval-region pins disabled, matching the existing ARM64 wide-local bound and preventing a hostile mixture of many scalar and vector locals from exhausting the register file before lowering begins. The bound depends only on validated local count, never module, function, producer, hash, or corpus identity. Native AMD64 tests cover direct FP-pin relinquishment and the real wide mixed-local regression fixture, assert one compile attempt per function, and the complete fuzz-regression corpus passes without retry. Ten alternating native Ryzen 7 7800X3D `GOGC=off` pairs on `many_funcs` and `json-as`, with native compaction both off and on, keep bytes and allocations exactly unchanged; compile-time geomean improves by 1.53%, with both JSON cases statistically unchanged.
120. The failed-attempt ledger, retry report, and retry-only merge/reset helpers are deleted after the production retry path. `FunctionAttempts` remains as the smaller positive oracle: module and per-function tests assert that every function is compiled exactly once. This removes 117 net lines of dead telemetry and prevents the diagnostics representation from preserving a state the compiler can no longer enter; generated code and lowering policy are unchanged.
121. Serial and parallel compilation no longer zero each completed `funcHints` record. Both retained hint layouts are pointer-free, their module-owned backing remains live until the compile call returns, and no later phase reads completion state from a zero record, so the stores released neither Go-scanned memory nor backing capacity. This deletes eight redundant writes across the two backends without adding replacement state. Ten alternating `GOGC=off` pairs on `many_funcs` and `json-as`, with native compaction both off and on, keep bytes and allocations exactly unchanged. Compile-time geomean improves by 1.40% on native AMD64 and 1.11% on native ARM64. The deletion is unconditional and introduces no selector.
122. ARM64 last-get hints are now retained only for functions that pass the bounded interval-region storage admission, matching the existing AMD64 lifetime boundary. Module construction compares the logical backing-payload cost of a dense local array with a compact admitted-local payload plus sorted range records and keeps the smaller representation; ranges and values share one `[]uint32` backing, so sparse mode adds no second allocation or slice header. Dense storage remains the fallback when most functions qualify. Parallel workers resolve sparse ranges by the retained local-score offset, independent of scheduling order. On a 1,024-function stress module with 64 locals per short ineligible function, ten alternating native ARM64 samples reduce hint-scan allocation from approximately 544.0 to 288.0 KiB/op (-47.1%), remove one of three allocations, and move median time from 75.4 to 68.8 microseconds (-8.8%). Complete `many_funcs` and `json-as` screens reduce allocation by approximately 1.7% and 0.7%, respectively, and remove one allocation; their four-case compile-time median geomean moves by approximately +0.5%. All 64 decodable corpus modules retain identical native-code lengths and hashes. Admission uses only the existing target option, body length, validated local count, and module EH semantics; representation choice uses only calculated byte counts and contains no workload selector.
123. Both backends now retain at most the first 64 local-score words for functions whose validated local count already disables whole-function pinning and whose body is ineligible for interval-region analysis. The first 64 remain available for entry-initialization facts; an interval-eligible wide function still retains its complete score and last-use vectors. Empty GP/FP pin pools also bypass candidate scans, so the compact view is never indexed as a full local vector. On a generated 1,024-function module with 256 locals per short function, ten paired-process samples reduce hint-scan allocation from approximately 1,081.4 to 294.9 KiB/op (-72.7%) with the same two allocations; native ARM64 median time falls from 103.0 to 72.0 microseconds (-30.1%), while an AMD64 Rosetta check falls from 223.2 to 134.7 microseconds (-39.6%). A ten-pair ARM64 screen over `many_funcs` and `json-as`, with native compaction off and on, leaves allocation unchanged and moves the four-case median geomean by approximately +0.7%. All 64 decodable corpus modules retain identical native-code lengths and hashes on native ARM64 and AMD64 under Rosetta. Native AMD64 timing remains pending because the configured `hub@hub` host timed out. Storage admission depends only on the target option, body length, validated local count, and module EH semantics; it contains no workload selector.
124. ARM64 direct-call lowering now reads the exact preserves-caller-pins classification from a spare bit in the already-retained 32-byte callee summary instead of allocating and retaining a second `[]bool` table for every module. The original-body classification is computed once and shared by all callers; a function whose calls disappear through inlining still performs the existing effective-hint classification for its own entry, preserving that distinct post-inline case. Ten alternating native ARM64 `GOGC=off` pairs reduce `many_funcs` by 320 B/op and one allocation and `json-as` by 48 B/op and one allocation, with all four ordinary/compact median time changes between -0.68% and +0.68%. All 64 decodable corpus modules retain identical native-code lengths and hashes. The bit records only the existing signature, local-count, call, memory, and global-access predicate and contains no workload selector.
125. ARM64 module call relocations now use one compact source-ordered ownership table instead of retaining a 24-byte slice header for every function. Serial compilation stores one checked 32-bit end offset per function over the existing flat relocation arena, reducing index storage to four bytes per function; parallel compilation adds no replacement index or copy and reads the already-validated ranges in each `funcResult` directly from the owning worker arena. Native ARM64 serial `many_funcs` falls from 73,488 to 66,576 B/op (-9.4%) with the same 29 allocations, while `json-as` falls by 976 B/op with unchanged allocation count. Parallel `many_funcs` falls by approximately 8.0 KiB/op and one to two allocations, and `json-as` by approximately 1.0 KiB/op and one allocation. Ten alternating native serial pairs are flat at +0.01% median with compaction disabled; the compact serial screen is -0.39%, and longer parallel screens remain within -0.83% to +0.67%. All 64 decodable corpus modules retain identical native-code lengths and hashes. Ownership selection follows only the existing serial or parallel scheduler topology; relocation lookup has no module, function, producer, or corpus selector.
126. ARM64 deferred-child links now use the same stable 32-bit chunk-and-slot coordinates as physical stack links, making the entire common operand-node backing pointer-free and reducing `elem` from 56 to 48 bytes (-14.3%). Unlike the rejected first child-ID prototype, hot consumers resolve each child once and carry the pointer view through the lowering decision instead of repeatedly decoding two-level links. Across five alternating `GOGC=off` pairs for all 36 compile-corpus modules, compile time improves by 0.29% geomean and the sum of module-median allocation falls from 38.92 to 35.45 MiB (-8.91%). Ten longer alternating pairs resolve the positive short-screen outliers to +0.12% on esbuild, +0.33% on `fib_rec`, -1.20% on `swar-pack-parse`, +1.27% on wasm3, and -0.60% on the synthetic multiply-high fixture. The eight-function/65,536-node parallel stress case falls from approximately 42.26 to 38.05 MiB/op (-9.96%) and improves median compile time by 8.7%. A structural test rejects any Go-scanned field in `elem`, chunk-growth/reset tests cover coordinate stability, the full ARM64 backend suite passes, and all 64 decodable corpus modules retain identical native-code lengths and hashes. Every ARM64 node uses the representation; there is no function or workload admission path.
127. ARM64 operand nodes now use their 24-byte storage record as a tagged variant payload: values retain the existing location fields, while deferred nodes place two child IDs in the otherwise-dead constant word and their opcode plus bounded depth in the otherwise-dead slot word. The result type and value-fact bits already have the same meaning in both variants, and two previously unused metadata bits encode the four node kinds. Physical links occupy the remaining eight bytes, reducing the common pointer-free `elem` from 48 to the plan's 32-byte target and from the original 112 bytes by 71.4%. Five alternating `GOGC=off` pairs across all 36 compile-corpus modules reduce summed module-median allocation from 35.45 to 23.53 MiB (-33.62%) and improve compile-time geomean by 0.55%; ten longer pairs resolve the three positive short-screen outliers to -0.19% for `quicksort`, -2.31% for SHA-256, and -0.20% for `swar-pack-parse`. The eight-function/65,536-node parallel stress case falls from 38.06 to 27.59 MiB/op (-27.51%), removes eight allocations, and improves median compile time by 2.16%. A controlled 1,048,576-node retention probe reduces added `/gc/scan/heap:bytes` from approximately 58.72 MiB for the pre-ID 56-byte backing to 3.5-6.2 KiB of runtime noise. A focused variant test pins kind, opcode, depth, both children, type, facts, and GC/EH root bits; the complete ARM64 backend suite passes; and all 64 decodable corpus modules retain identical native-code lengths and hashes. The representation applies to every ARM64 operand node without unsafe layout, per-node allocation, function admission, or workload selection.
128. ARM64 now bounds the complete pending operand packet, not only each individual tree. Before creating another deferred operation at 32 live deferred nodes, it materializes the oldest logical root, preserving Wasm trap order, and continues without retry; 64 remains a fail-closed hard limit. Consumed child IDs are threaded through their own dead link words as an allocation-free free list, so later operations reuse unreachable arena slots instead of retaining one node for every operation in the function. An adversarial function with 96 simultaneous deferred roots reaches the soft cap, never exceeds 32 live deferred nodes, and compiles in one attempt. Across five `GOGC=off` samples of wasm3, `json-as`, BLAKE, UTF, SQLite, Ruby, and esbuild, allocation falls by 39.79% geomean and allocation count by 1.92%, while compile time moves by +0.59% geomean. Ten longer samples resolve the large-module timing screen to +0.90% on SQLite and +1.47% on esbuild; their three-case geomean with wasm3 is +1.45%, inside the 1.5% memory-change gate. Generated-code byte counts remain identical in every measured row, the complete ARM64 backend suite passes, and the full executable corpus preserves results, traps, memory, and host effects. The limits and recycling apply uniformly to every function and add no workload admission path.
129. Exact native GC-root planning now has the plan's bounded `RootAllCanonical` mode on both architectures. A collecting function with at most four collector-reference locals uses conservative local masks only while `roots * body bytes <= 512`; a root-free collecting function always uses the cheaper site-counting path, while larger populations and bodies retain the exact CFG/dataflow planner. This bounds additional local-root retention and serialized offsets independently of module identity; EH keeps its already-required conservative masks. Eight alternating native pairs reduce the 1,024-function collecting planner by 27.73% on ARM64 and 27.22% on AMD64, allocated bytes by 52.74% and 52.42%, and allocations by 24.98% and 14.28%. The single collecting-function plan improves by 22.50%/29.87% and removes 45.28%/45.76% of planner bytes; complete single-function compilation is flat on ARM64 and 6.47% faster on AMD64 while saving one allocation on both. A native AMD64 frame-root workload remains allocation-free at execution, improves by 1.24%, and retains the identical 860-byte machine-code image. Tests prove the four-root and root-byte boundaries, exact compaction immediately above admission, both throughput and tiny-heap collection during native execution, hidden operand roots, local-start roots, and the unchanged 136-byte plan footprint. The conservative bit is compile-only; emitted metadata remains validated exact root coverage.
130. AMD64 operand nodes now reuse the value variant's otherwise-dead storage fields for deferred opcode, result type, bounded depth, register need, and node kind while retaining all four direct physical/tree pointers. Value nodes keep the ordinary storage-kind domain, while the sole non-value node state uses one unreachable high storage-kind sentinel; the old unproduced block and tombstone element states are deleted. Common value/deferred checks remain direct byte comparisons rather than decoded IDs or shifted metadata. This reduces `elem` from 64 to 56 bytes (-12.5%) and from the original 112 bytes by 50.0% without the coordinate lookups that made both AMD64 NodeID prototypes fail latency gates. Five alternating native Ryzen 7 7800X3D pairs across all 36 compile-corpus modules improve compile-time geomean by 0.56%, reduce heap by 5.78%, and leave allocation counts unchanged. Eight longer pairs over fannkuch, quicksort, wasm3, both json-as variants, Lua, SQLite, Ruby, and esbuild are statistically flat at +0.11% geomean while reducing heap by 6.18%; Ruby and esbuild are individually flat. The eight-function/65,536-node parallel stress compile improves by 3.76% and falls from 47.27 to 43.29 MiB/op (-8.41%); the ALU-heavy fixture improves by 5.65% and falls from 110.67 to 94.67 KiB/op (-14.46%). All 64 corpus modules retain identical native-code lengths and SHA-256 hashes, the executable corpus passes, and the native AMD64 backend, GC-root, default-bounds, and explicit-bounds suites pass. A layout and variant test pins the 56-byte record and proves that kind, opcode, type, depth, register need, facts, and GC/EH root bits remain independent. The remaining four pointers keep AMD64's backing Go-scannable; removing them still requires a holistic direct-index representation that passes the native latency gate.

A follow-up attempt to delete the now-redundant-looking `GCFrameRootPlan.Candidate` flag was rejected. Sparse module ownership represents RootNone with `nil`, so production currently constructs every non-nil plan as a candidate; removing the flag and its checks simplified both backends but did not change the 136-byte plan size, allocation bytes, or allocation counts. ARM64 planner and complete-compile time remained flat, but fifteen balanced native AMD64 pairs regressed the 1,024-function planner by 2.55% (p=0.019); the complete RootNone case moved by +0.80%, the collecting case was flat, and their three-case geomean moved by +1.09%. The complete prototype was removed. The explicit inactive state should be reconsidered only as part of a representation change that reduces footprint or recovers the AMD64 compile cost, not deleted for cosmetic simplicity alone.

A compact rank-and-bitset replacement for the dense module function-to-plan lookup was also rejected. It allocated one pointer-free 16-byte record per 64 local functions, used a population prefix and one bounded `popcount` to resolve each collecting plan, and allocated no lookup backing for an all-RootNone module. The isolated 1,024-function RootNone-heavy planner cut lookup allocation by 87.7% on ARM64 and 87.2% on native AMD64, but the accepted gate is complete compilation rather than the isolated stage. Fifteen balanced native AMD64 `GOGC=off` pairs showed the complete 1,024-function RootNone compile regressing from 947.8 to 975.3 microseconds/op (+2.90%, p<0.001), despite allocation falling from 301.3 to 297.3 KiB/op (-1.32%) and by one allocation. ARM64 complete compilation was statistically flat, but its isolated planner regressed 16.9%. The complete lookup prototype was removed; the new full-compile benchmark remains as a general guard for future RootNone-heavy module-state work. Any successor must avoid adding rank resolution to the per-function path and pass the complete-module latency gate. The prototype depended only on semantic may-collect classification and source order and contained no identity or corpus selector.

The remaining experimental AMD64 BMI2 `RORX` path was also re-audited rather than mechanically removed. Fifteen native Ryzen 7 7800X3D one-second samples put the dependent constant-rotate loop at 0.4339 ns/instruction with `RORX` versus 0.4350 ns/instruction with the baseline rotate, roughly a 0.25% advantage despite the two-byte larger fixture. Five alternating 300 ms pairs across all 36 executable corpus rows were aggregate-flat when the CPU-gated default was disabled, but SHA-256 regressed by 3.89% (p=0.008); SIMD BLAKE improved by 0.81% and every other row was statistically unresolved. The current CPUID and semantic constant-rotate selection therefore remains: it is a general target-feature path with a measured hot-workload benefit, not an identity- or corpus-selected exception. Removal would trade away qualified execution speed without reducing compile allocation.

A follow-up control-frame type-arena prototype was rejected. Replacing each frame's `[]machineType` with a checked 32-bit range reduced `ctrlFrame` from 88 to 72 bytes (-18.2%) and lowered compile bytes by roughly 0.4% across paired native ARM64 and AMD64 screens, but it required an extra lazy allocation on modules with multi-value block types and repeatedly indexed worker scratch during lowering. Native AMD64 `many_funcs/p4` regressed 2.17% across eight final pairs (p=0.028), beyond the memory-only gate, while code bytes remained identical. The complete prototype was removed. A future attempt must eliminate or inline type access structurally rather than trade a scanned frame header for another indirection.

A bounded AMD64 encoder-capacity prototype was also rejected. It reused the existing semantic count of direct-GC resolver sites to reserve up to 256 KiB for unusually expansive functions, without inspecting module identity or adding a retained hint field. On the 2,000-site GC layout compile fixture, ten native pairs reduced allocation from 4.432 to 3.533 MiB/op (-20.27%), improved time by 5.47-6.59%, and preserved the exact 242,372-byte native result. However, three independently rebuilt broad screens consistently moved `DeepScalarControl` by roughly +3.8-4.2% despite identical allocation there; the final ten-pair result was +3.82% (p=0.002). The complete source prototype was removed. This confirms that semantic native-byte estimates can eliminate encoder growth on expansion-heavy lowering, but they must be integrated without perturbing the ordinary scalar compilation path before they qualify as a default memory optimization.

An AMD64 compact physical-stack-link prototype was rejected as well. It ported ARM64's stable 32-bit chunk-and-slot IDs for `prev` and `next`, reducing the common AMD64 operand node from 64 to 56 bytes (-12.5%) while retaining pointer children and exact native code. Ten native Ryzen 7 7800X3D pairs across `many_funcs`, json-as, BLAKE, Lua, SQLite, Ruby, and esbuild reduced backend allocation bytes by 6.51% geomean, including 5.97% on Ruby and 8.80% on esbuild, with allocation counts unchanged. The extra ID decode on every physical-stack traversal nevertheless regressed backend compile time by 4.94% geomean: json-as +3.66%, SQLite +3.67%, Ruby +3.16%, esbuild +4.99%, and BLAKE +11.41%. Across the wider compile, compact, worker, full-pipeline, and throughput matrix, time regressed 3.48% while bytes fell 4.98%. The complete source prototype was removed. A future AMD64 NodeID migration must eliminate pointer-based list/tree traversal more holistically or recover direct-index locality; changing only the two list links does not pass the compile-latency gate.

A binary giant-function lane was also rejected. The prototype classified a function only when its conservative node hint exceeded the existing 1 MiB per-worker retention budget, then allowed one such function at a time while ordinary workers continued; both targets used the same resource rule and no workload identity. On an eight-function ARM64 stress module with 65,536 short-lived nodes per function and four workers, ten alternating fresh-process samples reduced median maximum RSS from approximately 44.0 to 31.8 MiB (-27.7%). It also regressed parallel compile time from 17.68 to 24.53 milliseconds (+38.8%), far beyond the balanced-default latency gate, while total allocation changed by only -0.03%. The production mutex and classification helper were removed. The general stress benchmark remains on both architectures. A successor must use weighted byte reservations or adaptive worker admission rather than turning a continuous memory budget into a one-function concurrency cliff.

An 8 MiB continuous weighted-admission prototype was rejected too. It charged each active function for its hint-derived node bytes, bounded encoder scratch, and control depth, retained largest-first order only for modules whose total estimate exceeded the budget, and allowed any mix of smaller functions that fit rather than using a binary giant class. On the same native ARM64 stress module, five alternating fresh-process samples reduced median maximum RSS from approximately 72.6 to 60.1 MiB (-17.3%) but moved median compile time from 18.11 to 18.71 milliseconds (+3.3%) and added six to seven scheduler allocations. It missed both sides of the acceptance gate: less than the required 20% RSS reduction and more than the allowed 2% latency regression. The implementation and tests were removed. The now-qualified pointer-free ARM64 child IDs instead reduce the live representation without suppressing concurrency; weighted scheduling should be reconsidered only after both backends have the final compact node cost.

Two AMD64 node-ID layouts were rejected on native hardware. The first replaced only physical-stack pointers and reduced corpus allocation by 6.51% but regressed compile-time geomean by 4.94%. The second converted all four physical and deferred links, reducing `elem` from 64 to 48 bytes and making the common backing pointer-free; direct first-chunk resolution avoided the ordinary two-level slice lookup. Across three alternating `GOGC=off` pairs over all 36 compile-corpus modules, summed module-median allocation fell by 15.42%, but compile-time geomean still regressed by 4.04%. The full native AMD64 backend suite passed, but both designs exceeded the 2% memory-only latency gate and were removed. AMD64 needs a representation that removes scanned links without inserting coordinate decoding into its hotter physical-stack traversals; ARM64's qualified child-only conversion does not imply that the same access trade is sound on AMD64.

An ARM64 Sethi-Ullman scheduling port was also rejected after same-binary qualification. It packed an exact bounded register-demand label beside the existing deferred depth without enlarging the 32-byte node, preserved Wasm trap order, skipped position-based interval regions, and ran only after established target-specific covers declined the tree. Five `GOGC=off` samples across all 36 compile corpora improved compile-time geomean by 0.41%, changed heap geomean by -0.06%, and left allocation counts flat. Execution nevertheless regressed by 1.25% geomean: scalar UTF slowed 35.09%, the combined SWAR loop 13.79%, and the two json-as SIMD exports 1.61-2.29%, outweighing a 5.49% fannkuch gain. A pressure-only successor found just two esbuild sites, both in a function that already had zero ordinary spills, so it had no causal resource win and was removed before broad benchmarking. The complete implementation, option, metadata, and tests are absent from production. ARM64's three-operand selector needs a costed whole-tree cover that accounts for target forms and critical path; copying AMD64's two-address child-order rule is not sufficient.

A restricted ARM64 pending-local-constant prototype was rejected too. It held at most eight scalar recipes in straight-line functions, collapsed repeated constant `local.set` operations, and rematerialized later `local.get` operations without allocating. The ARM64 backend and executable corpus passed, and an eight-pair same-binary `GOGC=off` qualification over regex, SQLite, Ruby, and esbuild was compile-neutral (-0.26% geomean, no significant individual change) with unchanged heap and allocation counts. Its generated-code benefit was only 64 bytes across nine representative modules: 16 bytes in Lua and 48 bytes in SQLite, with every other module unchanged. That does not repay a permanent per-worker table and inline-boundary state. A useful pending-binding design must share compact packet storage and cover more than constants instead of adding a separate fact plane.

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

At the reviewed snapshot, the common `elem` was 112 bytes on both architectures. The reconciliation work above moved rare pointer-bearing payloads into lazy sidecars and compacted the hot variants. ARM64 now has the plan's 32-byte pointer-free node and bounded packet recycling. AMD64 is 56 bytes—half the original payload—but deliberately retains four direct `*elem` links after two pointer-free ID layouts lost roughly 4–5% compile time on native hardware. Those AMD64 links remain the next source of pointer scanning and pointer-stability constraints; a successor must remove them without adding coordinate decoding to the hot traversal path.

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

## 5.5 Delete retry after qualification

The retry path served as a temporary oracle while the corpus and adversarial pressure cases identified the remaining exhaustion shapes. Production now compiles each function once. ARM64 reserves its general transient floor before emission; AMD64 combines bounded pre-emission admission with in-place integer, floating-point, and vector local-pin relinquishment. Wide-local functions begin canonically rather than speculatively reserving optional pins.

`FunctionAttempts` remains as the regression oracle. Focused register-hostile tests and module-level tests assert exactly one attempt per function; resource exhaustion must canonicalize local state or return an ordinary compile error, never restart emitted work.

## Phase 2 acceptance gates

- Exactly one production attempt per function on the complete corpus and adversarial pressure suite.
- At least 95% agreement between predicted pressure class and observed maximum pressure.
- No more than 1% execution geomean regression from reduced pinning.
- No individual hot benchmark more than 2% slower unless compile p99 improves enough to justify it.
- Register-hostile compile p99 materially lower.
- Failed-attempt state and telemetry do not exist in production.

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
