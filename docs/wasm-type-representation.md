# Compiler Wasm type representation

The compiler stores `HeapType`, `RefType`, `ValType`, `StorageType`, and
`FieldType` as the same pointer-free pair of `uint64` words. Constructors and
accessors in `src/core/compiler/wasm/types.go` are the only supported way to
interpret the bits.

The low word contains variant tags and flags:

| Bits | Meaning |
| --- | --- |
| 0..1 | heap-type variant |
| 8..15 | abstract heap type |
| 16 | recursive `TypeIdx` |
| 17 | resolved-definition coordinates present |
| 18..19 | resolved definition's component kind |
| 20 | nullable reference |
| 21 | exact reference |
| 22 | one-byte bare reference spelling |
| 23 | resolved component kind valid |
| 24..25 | value-type variant |
| 32..39 | numeric type |
| 40 | packed storage |
| 41..48 | packed storage type |
| 49 | mutable field |

The high word contains either the complete `uint32` type index or two complete
`uint32` resolved-definition coordinates (recursion-group and member). Ordinary
scalar shifts and masks are endian-independent. No unsafe reinterpretation is
used.

Two words are intentional. A resolved definition needs 64 payload bits before
its variant and validity tags are encoded. Reducing either coordinate would
narrow Wago's accepted domain. A one-word representation would therefore need
a module-owned side table and lifetime plumbing through every standalone value;
the measured two-word representation captures most of the density benefit with
substantially less ownership complexity.

The bare-spelling bit is binary metadata, not semantic type identity.
`EqualValType` ignores it, so `funcref` remains equal to the equivalent expanded
`ref null func` form. Resolved-definition component kind is cached only for
pointer-free consumers; definition identity remains its group/member pair.

Variable-length fields, parameters, results, and supertypes remain ordinary
dense slices owned by their composite/subtype. An arena was deliberately
deferred: leaf packing removes the dominant per-element cost without changing
slice ownership or public compiler lifetimes. Revisit an arena only if profiles
show backing-array allocation or retained slice metadata as a material remaining
cost.

## Dense-metadata measurement

`TestCompilerTypeRepresentationLayout` records the exact 64-bit size and Go
pointer containment of compiler metadata, import/extern descriptors, byte-backed
function metadata, and collector descriptors. `TestPublicTypeDescriptorLayout`
does the same for the exported structural descriptors retained by `Compiled`.

Import representation experiments use a fixed synthetic matrix of 10, 100,
1,000, and 10,000 function, global, table, memory, tag, and mixed imports. Run
the decode, validation, and five-kind iteration layers together so a smaller
descriptor cannot hide work moved into accessors:

```sh
GOMAXPROCS=1 taskset -c 2 go test ./src/core/compiler/wasm -run '^$' \
  -bench '^BenchmarkImportMetadata' -benchmem -benchtime=100ms -count=10
```

Use the same CPU and command for both sides of a comparison. Table and memory
fixtures declare maxima so pointer-to-scalar limit allocation remains visible.

`ExternType` is a 40-byte tagged payload rather than a 120-byte product of all
five external variants. Its constructors preserve full `uint32` type indexes,
full `uint64` limits, recursive indexes, table reference types, memory/table
address width, sharing, mutability, and explicit maximum presence. Consumers
must use the kind-specific accessors; inactive payloads have no meaning.

`Limits` stores its optional maximum inline with an explicit `HasMax` bit. This
keeps the 24-byte structure size unchanged, but makes `Limits`, `TableType`, and
`MemType` pointer-free and removes the per-table/per-memory maximum allocation.
No integer value is reserved as a sentinel.

On a Ryzen 7 7800X3D with Go 1.26.5, the packed representation changed the
10,000-import decode benchmark as follows (10 samples, one pinned CPU):

| Import kind | time | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| functions | 664.6 us -> 541.7 us (-18.5%) | 1520.1 KiB -> 736.1 KiB (-51.6%) | unchanged |
| tables | 1017.9 us -> 755.5 us (-25.8%) | 1645.1 KiB -> 736.1 KiB (-55.3%) | 20.01k -> 10.01k (-50.0%) |
| memories | 937.5 us -> 575.5 us (-38.6%) | 1645.1 KiB -> 736.1 KiB (-55.3%) | 20.01k -> 10.01k (-50.0%) |
| mixed | 713.5 us -> 625.2 us (-12.4%) | 1567.0 KiB -> 736.1 KiB (-53.0%) | 14.01k -> 10.01k (-28.6%) |

Direct validation of the packed payload avoids reconstructing table and memory
product values. At 10,000 imports, validation improved 40.0% for tables, 45.5%
for memories, and 32.1% for mixed imports. Function and tag validation changed
by +4.2% and +2.9%, respectively; their absolute changes were 5.1 and 3.7 us
per 10,000 imports. The complete focused matrix had a 9.5% time geomean
improvement and 19.1% B/op geomean reduction.
