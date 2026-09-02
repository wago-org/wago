# Engine-state differential fuzzing

The engine-state lane generates valid Wasm modules with Starshine. It runs each
module in Node and Railshot. Both sides record the same bounded event array and
compare its SHA-256 hash.

Build the Starshine WasmGC FFI once, if it is not present:

```sh
(cd ../starshine-mb && bun ffi build)
```

Run one complete 80-case cycle across all 30 profile leaves:

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
`../starshine-mb/dist/ffi/starshine-ffi.wasm`.

## Execution model

The shell script builds one persistent Go worker in the ignored
`.tmp/engine-state/` directory. Node loads the Starshine FFI once. For each case
it does the following work:

1. Generate a module from the root seed and one-based case index. A
   cross-instance case also generates one provider module.
2. Write the module, and its provider when present, to the run directory.
3. Execute its start function in Node and create the Node observation.
4. Send the paths, case seed, profile, and intended outcome to the persistent
   Railshot worker.
5. Compare the canonical SHA-256 hashes.
6. Delete a passing module, unless `--keep` is set.

The worker loads the custom `__fuzz` harness plug-in once. Each case gets fresh
imported global, memory, and table resources. Cross-instance cases instantiate
a provider first and import its exported resources into the consumer. The
provider does not attach to the pending harness case. The worker enables Wago
Core 3 features. One worker permits only one active case. The raw event buffer
is limited to 8,192 entries. Observation is limited to 2 memory pages and 32
table entries. These limits match the Starshine engine-state profile and keep
memory use predictable.

The exact cycle adds forced shapes for passive segment lifecycle, instantiation
boundaries, multiple memories and tables, wide and deep stacks, overlapping and
mixed-width memory access, indirect-call traps, nested trap placement,
cross-instance resource identity, failed bounded growth, and decoder topology.
It also contains one slot each for tail calls, typed function references,
memory64, exceptions, and GC. These proposal cases reduce their final result to
the same scalar and resource event schema.

The event array starts with this schema marker:

```json
["schema","starshine.engine-state-events.v1"]
```

It then records ordered input, mark, and value events from the `__fuzz` ABI. The
tail records the return, runtime trap, or pre-start instantiation-failure class.
A pre-start failure has no resource snapshot. Other outcomes are followed by
every synthetic exported global, memory, and table in Wasm index order. Integer
values use fixed-width lowercase hex. Each memory records its byte length and
SHA-256 hash. A live table function records `funcidx:N` when the instance is
available. After a trapping start, JavaScript does not expose the partial
instance. Its retained imported table can therefore record only the portable
`null` or `non-null` relation.

Engine names, elapsed time, error text, file paths, and seeds are not part of the
canonical hash. The Node and Go implementations are separate. A shared golden
test fixes only the event bytes, hash, and deterministic input mixer.

For runs of at least 80 cases, the driver rejects a Starshine cycle that omits
any required profile. A second `COVERAGE` line prints the bounded count for each
profile; it does not retain per-case data.

The success line prints one `result` hash for the run. Its input is the ordered
ASCII sequence `case_index:case_hash\n`. The line also prints the root seed and
the Starshine FFI hash. These values identify a run without retaining its case
files.

## Failure artifacts

Passing runs retain no case files by default. On the first difference, the lane
keeps the failing `.wasm` and writes a neighboring `.wasm.failure.json`. A
cross-instance failure also keeps `.wasm.support.wasm`. The JSON contains both
event arrays, both hashes, root and case seeds, the selected Starshine profile,
the FFI binary hash, support-module hash when present, and generator facts.

Reproduce case 842 directly with:

```sh
scripts/fuzz-engine-state.sh --count 1 --start 842 --seed ROOT_SEED --keep
```

## Current measurement

On the development Linux/amd64 host on 2026-09-02, a retained 10,000-case run of
the 30-profile cycle took 7.44 seconds inside the lane, or 1,344.1 cases per
second. The result hash was
`sha256:60bf0bd3e0f9867dcdffe3d7f0038aa5bccf680dacfe06d7ee0c7881dc5edd5d`.
The run covered each profile at its exact declared weight and had no output
difference. This includes generation, both executions, state hashing, worker
exchange, and passing-file writes. This is a workflow measurement, not a stable
engine benchmark.
