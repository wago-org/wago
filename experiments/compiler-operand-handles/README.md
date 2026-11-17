# Compact operand-handle PoC

This comparative experiment replaces AMD64 operand-node links, not optimization
policy or emission order. It is not a claim of a compilation speedup or target
acceptance.

## Representation

- `elem`: 40 bytes instead of 56; four `uint32` links replace four pointers.
- `storage`: unchanged at 24 bytes; plugin metadata stays in the scanned cold sidecar.
- `stack`: 128 bytes instead of 112; the additional state caches the last
  cross-chunk owner and counts checked-search fallbacks.
- IDs encode chunk and offset-plus-one in fixed 16-bit fields. Zero is absent.
- Owning chunk slices retain heap storage. The optional mapped arena still has
  its explicit owner; fallback never reconstructs a pointer from an integer.
- Existing short-lived pointers remain. Converting them to IDs has a cost:
  active/cached chunk checks and, on misses, a bounded-by-chunk-count search.
  This tradeoff must be measured before adoption.
- List edits preserve IDs directly where possible; pointer-to-ID conversion is
  not needed for the neighbor assignments in `erase`.

## Qualification so far

Standard Go 1.27.1, Linux/AMD64, `GOMAXPROCS=2`, `-p=1`:

```sh
go test -p=1 ./src/core/compiler/backend/railshot/amd64 -run '^TestOperandHandle' -count=1
go test -p=1 ./src/core/compiler/backend/railshot/amd64 -run 'Test(Stack|Operand|Tree|Deferred|MappedOperand|ScratchClear|Valent|LocalRead|FixedSelf|IfPrefix|PureSelect|Inline|TailLoop)' -count=1
go test -p=1 ./src/core/compiler/backend/railshot/amd64 -count=1
```

All pass. The first full run identified two obsolete exact layout assertions;
those now require the new sizes. The final default package run is captured in
`default-amd64.log`; the focused run is in `focused.log`.

Still pending: checker/profile build variants, TinyGo qualification, full corpus
native-code equivalence and execution oracles, fresh-process compilation/RSS
measurement. The full compiler redesign targets remain unproven.
