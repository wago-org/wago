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
