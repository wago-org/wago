# Wasmtime WebAssembly Core 3 corpus

This corpus exactly replays 103 applicable Core 3 tests from Wasmtime's
`tests/misc_testsuite` at revision
`e8ac8c27f19939bfb1d26d920368d8b6028a67a9` (August 1, 2026).

The checked-in `source.wast` files are unmodified upstream inputs. Most fixtures
contain deterministic binary modules and command JSON produced by the pinned
WebAssembly/spec 3.0 interpreter at revision
`9d36019973201a19f9c9ebb0f10828b2fe2374aa`, using
`scripts/spec-interpreter-json.py`. Files using WAST syntax that interpreter
cannot translate use the parser-independent fallback
`scripts/wasm-tools-wast-json.py`, pinned to wasm-tools 1.251.0; the fixture's
`commands.json` records which generator produced it.

Coverage includes WasmGC arrays, structs, casts, i31 references, recursive
function types, typed function-reference tables and indirect calls,
multi-memory aliasing, memory64, table64, exception handling, and SIMD values
inside GC and memory64 operations. Wasmtime `thread`/`wait` command graphs that
only require isolated Store retirement are replayed in a separate Runtime and
collector domain. `src/wago/wasmtime_core3_port_test.go` replays every fixture
with `CoreFeaturesV3` in an isolated process.

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

No applicable file remains pending. `RUNTIME_REUSE.tsv` gives a per-path
source-hash proof for all 104 existing runtime ports: 103 are byte-identical to
the revision pinned here, while `no-panic-on-invalid.wast` differs only in the
non-normative `assert_invalid` description; its quoted malformed module and the
preserved direct-invalid artifact are unchanged. `ADAPTATIONS.tsv` maps each of
the 5 files whose Wasmtime host hooks, resource policy, mixed proposal matrix,
or backend assumption cannot be replayed literally to the equivalent Wago
product test.
The unmodified upstream sources are retained under `adapted/` and pinned by a
separate tree digest. Where a mixed-feature or resource boundary permits a
source-level projection, `adapted/*/equivalent/` also retains deterministic
commands and binaries: `big-memory-behavior` replays all six state transitions
at the finite 1/2-page boundary, `memory-combos` replays all 64 assertions for
its four unshared standard-page memory32/memory64 members, `memory_fill`
replays all 26 standard-page and empty-memory assertions, and
`memory64/more-than-4gb` replays all eight modules and seven observations at the
last supported page while preserving shared-memory, global-offset, immediate-
offset, and large-memarg behavior. `memory64/table-too-big` replays the exact huge delta with Core's
all-ones failure result and unchanged size in both bounds products; its companion
direct product test also checks the exact huge minimum fails before descriptor
allocation because Wasmtime's expected host-allocation trap is not Core
`table.grow` semantics.

The outside-Core-3 set is limited to component-model execution, stack switching,
threads/atomics, shared-everything threads, custom page sizes, and wide
arithmetic. The two nonstandard files assert Wasmtime's optional NaN
canonicalization configuration rather than WebAssembly Core semantics.

All 103 exact Core 3 sources and all 5 retained adapted sources are
byte-identical to the revision pinned here. The four historical Embenchen
runtime fixtures no longer carry Wago-added `register` commands: named module
instances now satisfy their `env` imports directly, so their checked-in sources
are also exact upstream bytes. `RUNTIME_REUSE.tsv` records the sole remaining
source-text drift and why it does not change the replayed malformed module or
assertion semantics.
