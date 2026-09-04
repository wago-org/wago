# Engine-state differential fuzzing

The engine-state lane generates valid Wasm modules with Starshine. It runs each
module in Node and Railshot. Both sides record the same bounded event array and
compare its SHA-256 hash.

The repository includes the pinned Starshine WasmGC FFI at
`tests/enginefuzz/starshine-ffi.wasm`. It was built from Starshine commit
`1b46f450aa3adc252409bfa611514140cf91cd66` and has SHA-256 hash
`e4bb3aed71e8bcaa3b0fda634024bf0c46e8881353d13e1dcc69df2460288b18`.

To refresh it from a sibling Starshine checkout:

```sh
(cd ../starshine-mb && bun ffi build)
cp ../starshine-mb/dist/ffi/starshine-ffi.wasm tests/enginefuzz/starshine-ffi.wasm
```

Run one complete 136-case cycle across all 45 profile leaves:

```sh
scripts/fuzz-engine-state.sh
```

Run a larger reproducible sample:

```sh
scripts/fuzz-engine-state.sh --count 100000 --seed 0xf00dcafe12345678
```

Use `--seed random` to select and print a new root seed. Use `--start N` with the
same root seed to split a run or reproduce one case. `make fuzz-engine-state`
is an alias. Pass options through Make with `ENGINE_FUZZ_ARGS`, for example:

```sh
make fuzz-engine-state ENGINE_FUZZ_ARGS="--count 1000 --seed random"
```

Set `STARSHINE_FFI_WASM` to use a non-default FFI binary. The default is
`tests/enginefuzz/starshine-ffi.wasm`.

## Execution model

The shell script builds one persistent Go worker in the ignored
`.tmp/engine-state/` directory. Node loads the Starshine FFI once. For each case
it does the following work:

1. Generate a module from the root seed and one-based case index. Some cases
   also generate an ordered support-module chain or an equivalent comparison
   module.
2. Write the generated binaries to the run directory.
3. Execute the primary module in Node. Execute its comparison twin when one is
   present.
4. Send the paths, case seed, profile, and intended outcome to the persistent
   Railshot worker.
5. Compare both engines' canonical SHA-256 hashes. For a twin case, also require
   both distinct modules to produce the same hash in each engine.
6. Delete a passing module, unless `--keep` is set.

The worker loads the custom `__fuzz` harness plug-in once. Each case gets fresh
imported global, immutable initialization global, memory, and table resources.
Cross-instance cases instantiate an ordered provider and relay chain, then
import the final exports into the consumer. Support modules do not attach to the
pending harness case. The worker enables Wago
Core 3 features. One worker permits only one active case. The raw event buffer
is limited to 8,192 entries. Observation is limited to 2 memory pages and 32
table entries. These limits match the Starshine engine-state profile and keep
memory use predictable.

If a Railshot case exceeds `--timeout-ms`, the driver kills the persistent
worker and writes the case as the first failure. Killing the worker makes the
deadline effective even when native Wasm execution cannot return to the worker
protocol. The lane stops after its first failure, so it does not restart that
worker.

The exact cycle includes the original execution and proposal leaves. It also
forces 15 broader module shapes:

- multi-module link graphs with zero to two re-export relays;
- cyclic GC structs, mutable arrays, i31 values, and dynamic reference tests;
- extended constant expressions plus active and passive initialization graphs;
- exception unwind through direct, indirect, typed-reference, and tail calls;
- mixed result types across direct, indirect, `call_ref`, and tail calls;
- function-index and custom-section LEB boundaries;
- funcref, externref, typed funcref, and table64 behavior;
- mixed memory32 and memory64 modules;
- committed state before four trap families;
- independently encoded, semantically equivalent module twins;
- function-count, local-count, and control-depth compiler thresholds;
- stable NaN classification and signed-zero results;
- malformed binary families with strict compile-failure classification;
- bounded direct and mutual recursion with typed multi-value control joins; and
- branched nominal GC subtype graphs with successful casts and cross-sibling
  `ref.test` checks.

All executable cases reduce their result to the same scalar and resource event
schema. The invalid-module cases reduce compile errors to an expected family.

The event array starts with this schema marker:

```json
["schema","starshine.engine-state-events.v1"]
```

It then records ordered input, mark, and value events from the `__fuzz` ABI. The
tail records the return, runtime trap, pre-start instantiation-failure, or
compile-failure class. A pre-start or compile failure has no resource snapshot.
Other outcomes are followed by every synthetic exported global, memory, and
table in Wasm index order. Integer
values use fixed-width lowercase hex. Each memory records its byte length and
SHA-256 hash. A live table function records `funcidx:N` when the instance is
available. After a trapping start, JavaScript does not expose the partial
instance. Its retained imported table can therefore record only the portable
`null` or `non-null` relation. Externref tables also use this portable nullness
relation because their identities are host-local.

Engine names, elapsed time, error text, file paths, and seeds are not part of the
canonical hash. The Node and Go implementations are separate. A shared golden
test fixes only the event bytes, hash, and deterministic input mixer.

For runs of at least 136 cases, the driver rejects a Starshine cycle that omits
any required profile. A second `COVERAGE` line prints the bounded count for each
profile; it does not retain per-case data.

The long lane also checks resource lifetime. A linked owner can hold a closed
consumer function in an imported table while that consumer imports memory and a
global from the owner. Closing the chain in reverse must release this cycle.
`TestReverseCloseReexportChainReleasesFuncrefCycle` keeps that case as a small,
deterministic runtime regression.

The success line prints one `result` hash for the run. Its input is the ordered
ASCII sequence `case_index:case_hash\n`. The line also prints the root seed and
the Starshine FFI hash. These values identify a run without retaining its case
files.

## Failure artifacts

Passing runs retain no case files by default. On the first difference, the lane
keeps the failing `.wasm` and writes a neighboring `.wasm.failure.json`. A
support-graph failure also keeps numbered `.support.N.wasm` files. A metamorphic
failure keeps `.comparison.wasm`. The JSON contains both event arrays and
hashes, root and case seeds, the selected Starshine profile, the FFI binary
hash, all support-module hashes, the comparison-module hash when present, and
generator facts.

Reproduce case 842 directly with:

```sh
scripts/fuzz-engine-state.sh --count 1 --start 842 --seed ROOT_SEED --keep
```

## Current measurement

On the development Linux/amd64 host on 2026-09-02, a 1,000,000-case run of the
45-profile cycle took 859.65 seconds inside the lane, or 1,163.3 cases per
second. The root seed was `0xf00dcafe12345678`, and the result hash was
`sha256:690a9a45aeb548c7248b24a9a53e0dd8393fa2d3ca07e6a70c0846eab1c11ed9`.
The run used Starshine FFI hash
`sha256:e4bb3aed71e8bcaa3b0fda634024bf0c46e8881353d13e1dcc69df2460288b18`,
covered all profiles at their declared cycle weights, and found no output
difference. An earlier attempt found the linked funcref resource cycle described
above; the complete rerun passed after its runtime fix. This includes generation,
both executions, state hashing, worker exchange, and temporary passing-file
writes. This is a workflow measurement, not a stable engine benchmark.
