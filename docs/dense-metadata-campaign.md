# Dense metadata optimization campaign

Date: 2026-08-07

Baseline commit: `0ec96116b762673f09bccabafa4a366f63c709e0`

Environment: Go 1.26.5, linux/amd64, Linux 7.0.0-28-generic, AMD Ryzen 7
7800X3D. Focused benchmarks used `GOMAXPROCS=1 taskset -c 2`, 10 samples,
`-benchtime=100ms`, and `benchstat`.

## 1. Executive summary

| Experiment | Decision | Primary reason |
| --- | --- | --- |
| `ExternType` / `Import` | RETAINED | 120 -> 40 B and 152 -> 72 B; import decode and allocation improve materially. |
| `CompType` / `SubType` arena | REJECTED | Post-decode shared backing adds a copy: decode +11% to +34%, B/op +8% to +80%. |
| Public type descriptors | REJECTED | Leaves are already pointer-free; exported literals, codec, and runtime consumers make a compact mirror duplicative or API-breaking. |
| Runtime `TypeDesc` arena | REJECTED | A range table would disrupt direct field access and codec shape; no range arena was retained. The independent temporary-lowering removal was retained. |
| Optional scalar packing | RETAINED | Pointer-free limits and `TypeMetadata`; annotated decode time -20.6%, allocs/op -64.6% geomean. |
| Byte-range metadata | REJECTED | Function decode already has constant allocation count; a 16 B/function range saving requires pervasive module-buffer lifetime plumbing. |

Retained production commits are `8549b96d` (external types), `61c060c7`
(GC descriptor lowering), `d3c97e8f` (optional type metadata), and `374948e4`
(packed import validation). Benchmark infrastructure is in `64cb9e32` and the
final observability commit.

## 2. Layout changes

All sizes are amd64 `unsafe.Sizeof` results. Pointer containment is determined
recursively by reflection; `unsafe` is confined to tests.

| Type | Before | After | Go pointers before | Go pointers after |
| --- | ---: | ---: | --- | --- |
| `wasm.TypeIdx` | 8 | 8 | no | no |
| `wasm.CompType` | 96 | 96 | yes | yes |
| `wasm.SubType` | 152 | 152 | yes | yes |
| `wasm.TypeMetadata` | 16 | 16 | yes | no |
| `wasm.Limits` | 24 | 24 | yes | no |
| `wasm.TableType` | 40 | 40 | yes | no |
| `wasm.MemType` | 32 | 32 | yes | no |
| `wasm.GlobalType` | 24 | 24 | no | no |
| `wasm.ExternType` | 120 | 40 | yes | no |
| `wasm.Import` | 152 | 72 | yes | yes (strings only) |
| `wasm.Expr` | 48 | 48 | yes | yes |
| `wasm.Func` | 104 | 104 | yes | yes |
| `wago.ValueTypeDescriptor` | 16 | 16 | no | no |
| `wago.ReferenceTypeDescriptor` | 12 | 12 | no | no |
| `wago.HeapTypeDescriptor` | 8 | 8 | no | no |
| `wago.StorageTypeDescriptor` | 20 | 20 | no | no |
| `wago.FieldTypeDescriptor` | 24 | 24 | no | no |
| `wago.DefinedTypeDescriptor` | 152 | 152 | yes | yes |
| `gc.FieldDesc` | 8 | 8 | no | no |
| `gc.TypeDesc` | 64 | 64 | yes | yes |

`ExternType` stores one active payload in 40 bytes. `OptionalTypeIdx` uses a
full `uint32` index plus recursion and presence bits. `Limits` uses a full
`uint64` maximum plus `HasMax`; neither optimization reserves a valid value.

## 3. Focused benchmark results

### External types and imports

At 10,000 imports:

| Fixture | time before -> after | B/op before -> after | allocs/op before -> after |
| --- | ---: | ---: | ---: |
| functions decode | 664.6 -> 541.7 us (-18.5%) | 1520.1 -> 736.1 KiB (-51.6%) | 10.01k -> 10.01k |
| tables decode | 1017.9 -> 755.5 us (-25.8%) | 1645.1 -> 736.1 KiB (-55.3%) | 20.01k -> 10.01k (-50.0%) |
| memories decode | 937.5 -> 575.5 us (-38.6%) | 1645.1 -> 736.1 KiB (-55.3%) | 20.01k -> 10.01k (-50.0%) |
| mixed decode | 713.5 -> 625.2 us (-12.4%) | 1567.0 -> 736.1 KiB (-53.0%) | 14.01k -> 10.01k (-28.6%) |
| tables validate | 116.65 -> 69.96 us (-40.0%) | unchanged | unchanged |
| memories validate | 102.33 -> 55.82 us (-45.5%) | unchanged | unchanged |
| mixed validate | 111.06 -> 75.38 us (-32.1%) | unchanged | unchanged |

The complete 72-benchmark matrix improved 9.5% in time and 19.1% in B/op
by geomean. Function/tag validation at 10,000 imports changed +4.2%/+2.9%
(+5.1/+3.7 us absolute); large iteration improved 15.9% to 24.6%, although
10--1,000-entry iteration regressed about 18.5% to 22.8%. Iteration remains
allocation-free; the retained decode/validation/memory gains dominate.

### Composite backing arrays

The rejected shared-backing prototype performed a strict ordinary decode, then
copied fields, values, and supers into three shared arrays. Decode results:

| Shape | time change | B/op change | allocs/op change |
| --- | ---: | ---: | ---: |
| 10 types / 64 fields | +11.2% | +79.9% | +4.2% |
| 100 / 16 | +14.4% | +60.6% | +0.5% |
| 1,000 / 4 | +23.5% | +26.3% | +0.05% |
| 10,000 / 1 | +34.0% | +8.2% | effectively zero |

An efficient range implementation would need a counting prepass or a rewrite of
all type consumers around module-owned arenas. The low-disruption experiment
failed, so that complexity was not retained.

### Runtime descriptor lowering

| Shape | time before -> after | B/op before -> after | allocs/op before -> after |
| --- | ---: | ---: | ---: |
| 10 types / 64 fields | 3.603 -> 3.551 us (-1.4%) | 8,224 -> 8,032 (-2.3%) | 13 -> 10 (-23.1%) |
| all four shapes geomean | 61.39 -> 61.38 us (-0.02%) | 230.8 -> 229.5 KiB (-0.6%) | 160.8 -> 150.6 (-6.4%) |

Small temporary `[]StorageKind` values already stayed on Go's stack. The direct
builder is retained because it removes heap work for large structs with neutral
aggregate time and keeps layout arithmetic in the runtime package.

`StructRefOffsets` has no production caller; only tests and fuzzing call it.
There is therefore no repeated runtime offset allocation to optimize today.

### Optional type metadata

| Types | time before -> after | B/op before -> after | allocs/op before -> after |
| --- | ---: | ---: | ---: |
| 10 | 1.158 -> 0.929 us (-19.8%) | 2.672 -> 2.516 KiB (-5.9%) | 34 -> 14 (-58.8%) |
| 100 | 9.870 -> 7.831 us (-20.7%) | 20.53 -> 18.97 KiB (-7.6%) | 304 -> 104 (-65.8%) |
| 1,000 | 98.95 -> 77.98 us (-21.2%) | 196.6 -> 181.0 KiB (-8.0%) | 3,004 -> 1,004 (-66.6%) |
| 10,000 | 1.256 -> 0.996 ms (-20.7%) | 1.914 -> 1.761 MiB (-8.0%) | 30,004 -> 10,004 (-66.7%) |

### Function byte metadata

The rejected range baseline decodes 10,000 empty functions in 467.4 us,
1.071 MiB/op, and eight allocations/op. Allocation count is constant from 10
through 10,000 functions because bodies already retain subslices of the input.
Replacing the 24-byte slice with an 8-byte range would deterministically save
16 bytes/function, but would require raw-buffer ownership in `wasm.Module` and
module-context accessors through frontend, IR, both backends, and runtime feature
discovery. No production prototype was retained.

## 4. Existing pipeline

The exact repository Decode/Validate/Compile corpus comparison is generated by:

```sh
cd bench
GOMAXPROCS=1 taskset -c 2 go test -run '^$' \
  -bench 'Benchmark(Decode|Validate|Compile)$' -benchmem -count=10
```

| Phase | time before -> after | B/op before -> after | allocs/op before -> after |
| --- | ---: | ---: | ---: |
| Decode | 10.84 -> 10.82 us (-0.17%) | 7.427 -> 7.417 KiB (-0.13%) | 53.21 -> 53.21 (-0.01%) |
| Validate | 26.84 -> 26.50 us (-1.26%) | 6.660 -> 6.660 KiB (0.00%) | 33.86 -> 33.86 (0.00%) |
| Compile | 125.4 -> 124.3 us (-0.87%) | 207.0 -> 207.0 KiB (0.00%) | 264.0 -> 264.0 (0.00%) |

An initial final run exposed 47,517 allocations in SQLite validation versus
646 at baseline: returning reconstructed packed memory/global types by pointer
made their temporaries escape once per instruction. The run was rejected, hot
validation now reads only packed scalar properties, and the exact corpus was
rerun. Corrected SQLite validation is allocation-, byte-, and time-neutral.

## 5. Large-module results

The pipeline corpus includes the repository's checked-in large and real-module
fixtures. Type-heavy synthetic coverage additionally includes 10,000 imports,
10,000 annotated types, 10,000 one-field GC types, and 10,000 functions. The
external-type and optional-metadata results above are the clearest large-density
wins. External WasmGC corpora requiring `tests/spec-v3/test/core` could not run
because that pinned corpus is absent from this checkout.

| Workload/phase | time before -> after | B/op change | allocs/op change |
| --- | ---: | ---: | ---: |
| SQLite Decode | 4.364 -> 4.429 ms (+1.50%) | -0.41% | -0.04% |
| SQLite Validate | 13.99 -> 13.99 ms (neutral) | neutral | neutral |
| SQLite Compile | 59.13 -> 58.99 ms (neutral) | neutral | neutral |
| Ruby Decode | 45.42 -> 45.42 ms (neutral) | -0.13% | -0.01% |
| Ruby Validate | 176.3 -> 178.3 ms (+1.14%) | neutral | neutral |
| Ruby Compile | 647.9 -> 652.3 ms (not significant) | neutral | neutral |
| esbuild Decode | 33.23 -> 34.30 ms (+3.21%) | -0.02% | neutral |
| esbuild Validate | 108.7 -> 108.5 ms (neutral) | neutral | neutral |
| esbuild Compile | 407.3 -> 404.7 ms (-0.63%) | neutral | neutral |

An isolated esbuild Decode confirmation measured +1.73% rather than the full
run's +3.21%, with the same -0.02% B/op and unchanged allocations. This is an
acknowledged unrelated latency cost. It is outweighed by the 12--39% focused
import decode wins, 52--55% dense-import byte reductions, and neutral-to-better
phase geomeans, but should be watched for code-layout sensitivity.

## 6. GC and retained-memory effects

- Every retained `ExternType` removes 80 bytes; 10,000 imports remove 800,000
  bytes from the import array. Pointer-free inline limits also remove one tiny
  maximum allocation for every table or memory with a maximum.
- `TypeMetadata` remains 16 bytes but eliminates two GC-visible pointer slots
  and the decode/rewrite scalar objects. The 10,000-type fixture removes 20,000
  allocations.
- The descriptor builder removes temporary lowering allocations only; final
  `gc.TypeDesc` retained size and field ownership are unchanged.
- Deterministic pointer containment was measured. `runtime/metrics` scan bytes
  were not reported because no stable isolated measurement was obtained.

## 7. Binary footprint

| Profile | Before | After | Delta |
| --- | ---: | ---: | ---: |
| manager, stripped | 8,556,706 | 8,560,802 | +4,096 (+0.05%) |
| runtime-standard, stripped | 7,688,354 | 7,700,642 | +12,288 (+0.16%) |
| runtime-minimal, stripped | 7,323,810 | 7,340,194 | +16,384 (+0.22%) |
| runtime-minimal TinyGo, stripped | 1,896,680 | 1,899,560 | +2,880 (+0.15%) |
| manager, unstripped | 12,295,437 | 12,300,582 | +5,145 (+0.04%) |
| runtime-standard, unstripped | 11,159,601 | 11,176,971 | +17,370 (+0.16%) |
| runtime-minimal, unstripped | 10,656,252 | 10,678,134 | +21,882 (+0.21%) |
| runtime-minimal TinyGo, pre-strip | 4,343,520 | 4,345,368 | +1,848 (+0.04%) |

The 0.04--0.22% binary cost is accepted for the 80-byte/import retained saving,
large decode allocation reductions, and pointer-free metadata. Generated JIT
instruction selection is unchanged; these commits alter metadata construction
and consumption, not native emission.

## 8. Rejected experiments

1. **Shared `CompType` backing arrays.** Promising because 10,000 one-field
   types decode with 20,004 allocations. Post-decode compaction added allocation,
   copying, and 11--34% latency, so the production prototype was reverted.
2. **Explicit compiler type ranges.** Could shrink `CompType` and `SubType`, but
   needs a new ownership model throughout validation, structural identity,
   frontend, and tests. It was rejected after the shared-slice precursor failed;
   no hidden index restriction or speculative plumbing was retained.
3. **Packed exported descriptors.** Leaf descriptors are already pointer-free.
   Exported field literals and codec shape are compatibility contracts; a second
   packed form would increase retained memory. No production change was made.
4. **Runtime field arena.** It could remove the remaining per-struct final field
   allocation, but `TypeDesc.Fields` is consumed directly by allocation, access,
   scanning, snapshots, codec, and public compiled metadata. The arena itself
   was rejected; only the independently measured lowering temporary was removed.
5. **Reference offset cache/bitmap.** Production code never calls
   `StructRefOffsets`; direct field iteration already serves scanning. A cache
   would add retained metadata for no production saving.
6. **Function byte ranges.** A range saves 16 bytes/function, but no allocation,
   and requires pervasive raw-module lifetime coupling. The production change
   was not made; its focused benchmark remains.

## 9. Remaining opportunities

Ranked by measured residual cost:

1. Eliminate singleton `RecType.SubTypes` allocations during decode. The
   10,000-type focused runs retain about one allocation/type after optional
   metadata packing; an inline singleton representation could target this
   without touching field arenas.
2. Build final `gc.FieldDesc` values into a shared arena at `Compiled` ownership
   boundaries. The 10,000-type lowering case still has about 3,356 allocations,
   dominated by final struct field slices; codec/runtime API migration is the
   main constraint.
3. Reduce import-name string objects or retain validated name byte ranges. The
   packed function-import decoder still performs about one allocation/import
   even though `ExternType` and limits no longer allocate.
4. Revisit direct, single-pass compiler type arenas only with a safe exact-count
   type-section prepass. The measured post-copy design is unacceptable, but
   20,004 allocations at 10,000 one-field types establish the residual ceiling.
5. Introduce module byte ranges behind a complete accessor migration, starting
   with functions. The measured deterministic ceiling is 16 bytes/function
   (156 KiB for 10,000 functions); proceed only if a real large-module retained
   heap profile shows that saving above the ownership complexity.

## Verification notes

Focused compiler, frontend, IR, backend, runtime GC, descriptor, codec-adjacent,
and layout tests pass. `go vet ./...` and the requested Linux/ARM64 no-run build
pass. After unrestricted rerun, the full suite's only failures are the missing
pinned `tests/spec-v3/test/core` cases. The exact final pipeline completed with
1,086 lines; raw baseline/final SHA-256 values are `8706cbb4e96e...` and
`73afe1ee7247...`.
