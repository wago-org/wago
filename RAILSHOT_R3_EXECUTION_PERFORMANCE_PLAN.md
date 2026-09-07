# Railshot R3 execution-performance plan

Status: proposed; measure each phase independently before retaining it.

Implementation progress:

- [x] Add pointer-free regional-residency debt telemetry without changing emitted
  code.
- [ ] Refresh paired execution, compile-resource, and native-size baselines using
  the new counters.
- [ ] Add the bounded local event tape and shadow residency planner.

Source: [shared Railshot Design conversation](https://chatgpt.com/share/6a9f2829-4380-83e8-b5ac-b9747c326b95).

This repository branch starts at `a07de0973191efab1d32677eff527952c7f9cdd2`.
The supplied research was audited against the older
`447f057115ee04d9e58580061dbee696becec21f`, so every source-level assertion and
benchmark baseline must be refreshed before implementation. `FEATURES.md`,
`ROADMAP.md`, and `OPTIMIZATIONS.md` remain authoritative when this proposal and
the current tree disagree.

## Objective and constraints

Improve Railshot execution latency across the full benchmark corpus, with
particular attention to register-pressure-heavy BLAKE, SWAR, calls, branches,
memory loops, and SIMD workloads.

The portfolio goal is a best-effort 30% execution-latency reduction. It is not a
forecast and must not be used to justify a regression elsewhere. The allowed
ceilings are:

- compile latency: no more than 1.25x;
- peak compiler memory: no more than 1.20x;
- native code size: no more than 1.10x;
- steady-state allocations in operation lowering: zero;
- correct Wasm semantics, GC visibility, trap order, and runtime safety: no
  regressions.

## Working diagnosis

Register residency is likely the largest broad remaining opportunity, but simply
pinning more locals is not the design. Function-wide hotness can retain the wrong
value version for too long, increase fixed-register interference, move spills,
and deny short-lived expressions the registers they need.

The replacement should plan residency by structured phase and physical debt:

- identify the exact local version covered by a lease;
- bound each lease to a loop, branch region, ARX round, or straight-line phase;
- explicitly track whether the canonical home is current and whether the value
  is dirty, borrowed, or visible to the collector;
- account for calls, fixed-register operations, transient pressure, merge
  reconciliation, and final-use ownership transfer;
- treat the plan as fail-soft guidance, never as a reason to recompile or fail.

## Proposed architecture

```text
validated Wasm function
        |
compact local/control/event prepass
        |
bounded structured residency planner
        |
one forward semantic and code-generation traversal
        |
Valent expression forest plus two physical plans per sink
        |
12-op normal or 24-op kernel machine window
        |
bounded finalization
```

This design does not add whole-function SSA, retained machine IR, a general CFG
optimizer, graph coloring, global linear scan, an online solver, per-operation
heap allocation, or a second machine-code candidate.

### Local event tape

Build a pointer-free, worker-owned event tape containing only information needed
for residency decisions: local reads and definitions, structured control
boundaries, branches, calls, collection points, invalidating effects,
fixed-register operations, and pressure boundaries.

Initial hard caps from the research are:

| Resource | Normal | Kernel |
| --- | ---: | ---: |
| Events | 32,768 | 65,536 |
| Structured regions | 256 | 512 |
| GP candidates | 48 | 64 |
| Vector candidates | 24 | 32 |
| Residency segments | 768 | 1,536 |

Exhausting a cap must fall back to coarse hints or direct-flush behavior without
unbounded allocation, a second Wasm traversal, or recompilation.

### Versioned, phase-sensitive leases

Detailed versions are created only for selected residency candidates. A lease
must identify a local, version, register, start and end event, estimated benefit,
call crossings, and state flags. Code generation must know whether the stack home
is valid, whether the register is borrowed, and whether reference visibility is
sufficient at the next collection point.

Rank segments by avoided physical work rather than raw function-wide frequency:

```text
loads and stores avoided
+ call-save traffic avoided
+ final-use transfers and destination copies avoided
+ address reuse
- home synchronization and merge cost
- fixed-register conflicts
- predicted transient pressure
```

At structured boundaries, use a deterministic bounded matcher over the resident
values and physical registers. Keeping a value in place is cheapest; moves,
reloads, dirty stores, fixed-register conflicts, and merge incompatibilities add
cost.

### Physical microplanning

At each semantically committed sink, compare at most two legal physical plans in
normal code and three in qualified kernel regions. A plan can choose evaluation
order, destination, destructive versus nondestructive form, immediate versus
register, folded memory operand versus explicit load, fixed-register setup,
rematerialization, address form, bounds discharge, flags versus materialized
boolean, and final sink.

Use a 12-operation no-reorder window normally. Qualified ARX kernels may use a
24-operation ready-set window, with an absolute maximum of 32, but cannot move
operations across traps, memory effects, calls, atomics, collection points, or
exception edges.

## BLAKE and SWAR lane

BLAKE should use round- and phase-specific residency:

1. retain active working-state versions;
2. load message words near the consuming round;
3. transfer ownership on final local reads;
4. release values not used in the next phase;
5. give independent quarter-round chains disjoint temporary groups;
6. avoid target fixed registers;
7. restore exact home and root state at calls and merges.

SWAR transformations require proof-carrying facts such as packed 32-bit lanes,
contained carries, known lane masks, and recognized lane rotates. Any operation
that can carry between lanes invalidates independence unless the surrounding
arithmetic proves containment. Do not restore removed producer-specific pattern
recognizers under new names.

Only after scalar residency is demonstrably correct should a bounded packet test
native-width lifting to AVX2, AVX-512VL/AVX10, NEON, or SVE2. Pack/unpack cost,
feature gates, constant-time behavior, and exact lane semantics are mandatory.
An explicit crypto plugin intrinsic remains the higher-ceiling option when the
guest can name the semantic operation directly.

## Other performance lanes

### Calls and ABI

- define finite, enforceable callee effect and clobber classes;
- retain selected local and descriptor leases only across proven leaf calls;
- lazily revalidate memory and table state on first post-call use;
- expand result registers only from measured signature distributions;
- sink results directly into locals, returns, comparisons, stores, block results,
  or following arguments;
- test paired ARM64 save/reload operations;
- defer exact acyclic-callee clobber masks until the finite summaries are proven.

### Memory and loops

- choose address formation and bounds discharge together;
- keep useful memory and table descriptors regionally resident;
- test running native pointers for proven induction variables;
- permit at most one guarded fast-loop clone with complete range preflight, a
  semantic slow path, and total module growth within the code-size ceiling.

### Branches and layout

- consume existing Wasm branch hints;
- measure optional compact profile sidecars;
- choose structured fallthrough from branch probability;
- test tiny if-conversion only for pure nontrapping regions;
- move trap, adapter, and cold collector paths out of hot layout;
- consider caller/callee clustering only after branch-local wins are established.

### Target policy

AMD64 experiments include physical-debt-aware BMI2 selection, memory operands
versus early loads, separately gated APX policy, and exact ARX SIMD kernels.
ARM64 experiments include a larger phase-specific residency budget, shifted and
extended operands, MADD/MSUB and conditional-select forms, pair transfers, tiny
kernel scheduling choices, LSE, MOPS, and strictly qualified SVE2 lifting.

## Implementation sequence

Each numbered item should be a separate reviewable PR or a smaller sequence when
required by safety. Shadow and telemetry phases must not change emitted code.

1. Refresh paired baselines and add physical-debt telemetry.
2. Add the pointer-free local event tape.
3. Run the residency planner in shadow mode.
4. Add versioned local leases and fail-soft eviction.
5. Make AMD64 residency fixed-register-aware.
6. Use ARM64's larger register file with phase-specific leases.
7. Add a generic BLAKE/SWAR ARX region plan.
8. Add generated target-rule metadata.
9. Add bounded two- and three-candidate physical planning.
10. Add the 12-operation no-reorder machine window.
11. Add the 24-operation qualified kernel scheduling window.
12. Add finite call effects and lazy post-call revalidation.
13. Plan addresses and bounds checks jointly.
14. Test one guarded structured-loop fast path.
15. Perform a SIMD debt census and test four-node vector packets.
16. Test native-width ARX lifting.
17. Test profile-guided pinning, fallthrough, and clustering.
18. Qualify BMI2, LSE, and MOPS policies.
19. Keep APX and SVE2 as separately gated experiments.
20. Evaluate an explicit crypto intrinsic plugin.

## Measurement and acceptance

Use exact candidate and base SHAs, identical build tags and toolchains, pinned CPU
affinity where available, `GOMAXPROCS=1`, alternating paired samples, and
distribution-aware comparison. Separate public Go-to-Wasm entry, prepared entry,
raw native transition, in-Wasm direct call, in-Wasm indirect dispatch, and
Wasm-to-host costs.

For every retained phase record:

- full execution-corpus geometric mean and per-workload deltas;
- compile latency, peak memory, allocation counts, and native bytes;
- spills, reloads, moves, fixed-register repairs, and lease transitions;
- AMD64 and ARM64 results;
- differential semantics, traps, GC roots, calls, merges, and codec behavior.

The full design is killed or reduced to isolated proven pieces if structured
residency improves the corpus by less than 3%, the first three active phases
improve execution by less than 8%, any budget ceiling is exceeded, operation
lowering allocates, correctness needs general live intervals or a retained CFG,
or BLAKE gains do not transfer to another ARX workload.

## Source artifact manifest

The final shared message references these nine artifacts:

1. `railshot_exec_v3/railshot_r3_exec_performance_plan.md`
2. `railshot_exec_v3/blake_as_swar_deep_dive.md`
3. `railshot_exec_v3/railshot_r3_experiment_matrix.csv`
4. `railshot_exec_v3/additional_sources.md`
5. `railshot_exec_v3/current_main_audit.md`
6. `railshot_r3_exec_performance_research_bundle.zip`
7. `railshot_exec_v3/blake_pin_ab_summary.md`
8. `railshot_exec_v3/pin_sweep_summary.md`
9. `railshot_exec_v3/blake_local_residency_simulation.md`

As of 2026-09-07, the public share renders their names and the complete final
plan, but ChatGPT's public file resolver returns `Code interpreter file links not
found` for each sandbox path. They therefore are not checked into this branch.
When working links or the original bundle are available, place the unpacked files
under `research/railshot_exec_v3/`, verify the archive against its contents, and
replace this note with checksums and provenance.
