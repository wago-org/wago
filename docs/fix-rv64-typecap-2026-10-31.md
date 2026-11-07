# RV64 type-cap fix — 2026-10-31

Base: `main` at `27abf4470e1137c18233d246e05dee6fb76ef266`

Branch: `fix/rv64-typecap`

Toolchain: `go version go1.27.3 linux/amd64`, from `$HOME/local/go/bin`

## Result

RV64 now admits more than 1,024 flattened type declarations without widening the 1,024-function policy. The regression fixtures pass at both 1,024 and 1,025 declarations through rooted wire and decoded admission, the internal reference directory, generic RV64, SWAR, hybrid production, control declaration, public RV64, validation, and amd64 control paths.

The new tests also cover a recursive member reference at local type index 1,024, a cross-group subtype whose supertype is local index 1,024, a dynamic tail plan that retains type index 1,024, and the closed-reference admission path above 1,024 types.

The verification report's format check still applies. Type indexes, recursive-group bases and sizes, and tail plan type indexes remain `uint32`. No encoding format changed.

## Changes by file

| File | Change |
| --- | --- |
| `module.go` | Added `MaxTypeDeclarations`, derived from the existing public code resource budget, and used it for type decoding. Kept `MaxFunctions` at 1,024. |
| `swar_module.go` | Separated decoder, decoded outer-type, and wire type-count limits from function and other resource limits. |
| `reference_function_module.go` | Separated both decoder limits and closed-reference outer type admission from function, export, element, and custom-section limits. |
| `reference_host_boundary.go` | Gave host-boundary decoding the type declaration budget. |
| `tail_import_source.go` | Separated type decoding and outer type admission while preserving function, import, export, and module-shape checks. |
| `tail_dynamic_source.go` | Separated decoder and outer type limits. Replaced the 1,024 type-index rejection with declaration-budget and source-directory membership checks. Tail site, frame, table, and invocation checks are unchanged. |
| `reference_types.go` | Replaced the flattened 1,024 cap with overflow-safe dynamic counting against the type resource budget. Kept empty-directory rejection and lookup bounds. Separated scalar, SWAR, and decoded type-directory cardinality from functions. |
| `gc_rooted_scalar.go` | Separated outer, flattened, parser, wire group, and cumulative member limits. Capacity is now derived from the checked wire group count. Preserved function, code, export, table, byte, arity, and `uint32` checks. |
| `reference_control_types.go` | Charged outer and flattened declared types against the type budget. Carrier-byte and arity accounting is unchanged. |
| `gc_immediate_references.go` | Separated outer type count from the function and profile resource limits. |
| `local_mutable_reference.go` | Separated outer type count while retaining the export limit. |
| `integer.go` | Separated the integer control type directory from the genuine reference function directory limit. |
| `source_function_types.go` | Allowed subtype index/key cardinality up to the type budget. The independent `MaxPublicCodeBytes/8` storage guard at lines 50-54 remains unchanged. |
| `reference_call.go` | Separated function-reference cast and test signature-key cardinality from function limits. |
| `table.go` | Separated indirect signature-key cardinality while retaining dispatch-mode checks. |
| `decoded_metadata_budget.go` | Gave type and field name maps type-specific count limits. Function, local, label, and other name maps keep their function-sized limits. The shared byte budget is unchanged. |
| `swar_function.go` | Replaced the literal 1,024 SWAR control type-directory cap with the type declaration budget. Code-work reservations remain unchanged. |
| `rv64_typecap_test.go` | Added committed 1,024/1,025 regression fixtures, all admission and backend controls, live index 1,024 reference cases, and the dynamic tail case. |
| `reference_function_module_test.go` | Updated the old closed-reference type boundary expectation while continuing to test the function, export, custom, segment, element, and local limits. |
| `reference_indexed_types_test.go` | Updated scalar, SWAR, and decoded type-directory boundary expectations. Arity limits remain covered. |
| `swar_indexed_test.go` | Updated the SWAR control type-directory boundary expectation. Invalid signatures and operand bounds remain covered. |

## Gates

| Gate | Result |
| --- | --- |
| Focused rooted and type-cap suite: `go test -json -count=1 ./src/core/compiler/backend/railshot/riscv64 -run 'Rooted\|RV64Type'` | PASS. 146 passed test/subtest events, 0 failed, 0 skipped. |
| Full touched package: `go test -json -count=1 ./src/core/compiler/backend/railshot/riscv64` | PASS. 4,056 passed test/subtest events, 0 failed, 117 skipped. |
| Railshot packages: `go test -count=1 ./src/core/compiler/backend/railshot/...` | RV64, ARM64, shared, and railshot root passed. The aggregate JSON run recorded 10,545 passed events, 1 failed event, and 801 skipped events across 4 passing packages and 1 failing package. |
| Build: `go build ./...` | PASS. 1 build gate passed, 0 failed. |

The sole railshot failure is `amd64/TestGCResolversLoadSpaceByte`. It requires `objdump`, which is absent from `PATH`. The exact test also fails on untouched `main` at `27abf4470` in a temporary detached worktree with the same diagnostic: `space-load code regression requires objdump: exec: "objdump": executable file not found in $PATH`. The temporary worktree was removed. This is baseline environment evidence, not a failure introduced by the RV64 change.

## Performance statement

**READY TO MEASURE.** The changed code is compile-time decoding, admission, directory construction, metadata accounting, and signature-set generation. It does not change an execution hot path. Newly admitted large type sets can do more compile-time work and allocate larger compile-time directories, so a no-perf-effect claim would be too strong. Measure compile time and allocation behavior on a valid large-type module, then run the standard compile benchmarks before merge.

Proposed workloads:

1. Compile a valid RV64 module with a large number of flattened type declarations, including recursive groups and subtype edges.
2. Run the repository's standard compile benchmarks for RV64 and the backend control architectures.

## Draft pull request description

RV64 rejected valid modules once they had more than 1,024 flattened type declarations. Validation and amd64 accepted the same modules. The limit came from reusing the function cap for type decoding and compile-time type directories.

This change gives types their own resource budget. It keeps the 1,024 limit for real functions, imports, tables, exports, calls, descriptors, and owner protocols. It also keeps byte budgets, code-size limits, arity checks, directory bounds, and `uint32` overflow checks.

The regression tests cover 1,024 and 1,025 declarations across the RV64 admission paths. They include recursive and cross-group references to local type index 1,024, closed-reference admission, and a tail plan using type index 1,024.

The touched RV64 package has 4,056 passing test events and no failures. The rooted and type-cap selection has 146 passing events and no failures. `go build ./...` passes. The broader railshot run has one unchanged amd64 environment failure because `objdump` is not installed. The same test fails on untouched main.
