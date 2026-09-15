# Wasmtime WebAssembly Core 3 corpus

This corpus exactly replays 103 applicable WebAssembly Core 3 tests from
Wasmtime's `tests/misc_testsuite`. The source pin is revision
`e8ac8c27f19939bfb1d26d920368d8b6028a67a9` (August 1, 2026).

Use this page to understand the source pin, conversion tools, coverage, and
completeness audit. It is an exact replay corpus. The inventory explains why a
file that is not replayed falls outside mandatory Core 3 or uses Wasmtime-only
behavior.

## Source and Conversion

The checked-in `source.wast` files are unmodified upstream inputs. Most fixtures
contain deterministic binary modules and command JSON from the pinned
WebAssembly/spec 3.0 interpreter, revision
`9d36019973201a19f9c9ebb0f10828b2fe2374aa`. The generator is
`scripts/spec-interpreter-json.py`.

Some WAST syntax cannot be translated by that interpreter. Those files use the
parser-independent `scripts/wasm-tools-wast-json.py` fallback, pinned to
wasm-tools 1.251.0. Each fixture's `commands.json` records its generator.

## Coverage

Coverage includes:

- WasmGC arrays, structs, casts, i31 references, and recursive function types;
- typed function-reference tables and indirect calls;
- multi-memory aliasing, memory64, and table64;
- exception handling; and
- SIMD values inside GC and memory64 operations.

Wasmtime `thread`/`wait` command graphs that only need isolated Store retirement
run in a separate Runtime and collector domain.
`src/wago/wasmtime_core3_port_test.go` replays every fixture in an isolated
process with `CoreFeaturesV3`.

## Completeness audit

`UPSTREAM_INVENTORY.tsv` classifies every one of the 369 `.wast` files under the
pinned `tests/misc_testsuite` tree:

| Status | Files |
|---|---:|
| Existing general runtime corpus | 104 |
| Exact Core 3 replay in this corpus | 103 |
| Provenance-linked Wago product adaptations | 5 |
| Outside mandatory Core 3 | 155 |
| Wasmtime-specific nonstandard semantics | 2 |

No applicable file is pending. `RUNTIME_REUSE.tsv` proves each of the 104
existing runtime ports by path and source hash. Of these, 103 are byte-identical
to this pin. `no-panic-on-invalid.wast` differs only in a non-normative
`assert_invalid` description; its quoted malformed module and direct-invalid
artifact are unchanged.

`ADAPTATIONS.tsv` maps each of the five files that cannot be replayed literally
because of Wasmtime host hooks, resource policy, mixed proposal matrix, or a
backend assumption. It maps them to an equivalent Wago product test. The
unmodified sources live under `adapted/` and have a separate tree digest.

When a mixed-feature or resource boundary permits a source-level projection,
`adapted/*/equivalent/` also retains deterministic commands and binaries:

- `big-memory-behavior` replays all six state transitions at the finite
  1/2-page boundary.
- `memory-combos` replays all 64 assertions for four unshared standard-page
  memory32/memory64 members.
- `memory_fill` replays all 26 standard-page and empty-memory assertions.
- `memory64/more-than-4gb` replays all eight modules and seven observations at
  the last supported page. It keeps shared-memory, global-offset,
  immediate-offset, and large-memarg behavior.
- `memory64/table-too-big` replays the exact huge delta. It expects Core's
  all-ones failure result and an unchanged size in both bounds products. Its
  direct product test also checks that the exact huge minimum fails before
  descriptor allocation. Wasmtime's expected host-allocation trap is not Core
  `table.grow` semantics.

The outside-Core-3 set is limited to component-model execution, stack switching,
threads and atomics, shared-everything threads, custom page sizes, and wide
arithmetic. The two nonstandard files check Wasmtime's optional NaN
canonicalization setting, not WebAssembly Core semantics.

All 103 exact Core 3 sources and all five retained adapted sources are
byte-identical to this pin. The four historical Embenchen runtime fixtures no
longer have Wago-added `register` commands. Named module instances now satisfy
their `env` imports directly, so their checked-in sources are exact upstream
bytes too. `RUNTIME_REUSE.tsv` records the only remaining source-text drift and
why it does not change the replayed malformed module or assertion semantics.
