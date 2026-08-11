# WebAssembly Core 3.0 default-feature audit

Date: 2026-08-11

## Scope and conformance boundary

The final Core 3.0 change history names eight binary/runtime feature families:
extended constant expressions, tail calls, exception handling, multiple
memories, 64-bit address spaces, typeful references, garbage collection, and
relaxed vector instructions. Profiles and custom annotations are two additional
Release 3.0 sections, but profiles describe coherent language subsets and
custom annotations affect the text format rather than binary execution.
([Core 3.0 change history](https://webassembly.github.io/spec/core/appendix/changes.html#release-3-0))

The specification's implementation-limitations appendix says a conforming
implementation cannot omit individual features. Therefore a Wago default made
from a subset of these families should be described as “selected Core 3
features enabled by default,” not as the complete Core 3 language. The existing
explicit `CoreFeaturesV3` group remains the right meaning for the complete
release surface.
([Core 3.0 implementation limitations](https://webassembly.github.io/spec/core/appendix/implementation.html))

## Feature risk

| Family | Normative surface | Default-admission risk | Assessment |
|---|---|---:|---|
| Extended constant expressions | Adds integer add/sub/mul and access to preceding immutable globals in constant expressions. ([spec change history](https://webassembly.github.io/spec/core/appendix/changes.html#extended-constant-expressions), [proposal source](https://github.com/WebAssembly/spec/tree/main/proposals/extended-const)) | Low | Additive and confined to validation/evaluation of module initializers. This is the clearest default-on candidate. |
| Tail calls | Adds `return_call` and `return_call_indirect`. ([spec change history](https://webassembly.github.io/spec/core/appendix/changes.html#tail-calls), [proposal source](https://github.com/WebAssembly/spec/tree/main/proposals/tail-call)) | Low–moderate | Additive control instructions with no new stored value family. Correctness still depends on genuine frame replacement across internal, indirect, host, and cross-instance boundaries. |
| Relaxed vector instructions | Adds relaxed SIMD operations whose result may be implementation-dependent. ([spec change history](https://webassembly.github.io/spec/core/appendix/changes.html#relaxed-vector-instructions), [proposal source](https://github.com/WebAssembly/spec/tree/main/proposals/relaxed-simd)) | Low–moderate | Additive opcodes and no new ownership model. Enable only where SIMD is executable. The Full profile permits the specified result alternatives; the Deterministic profile fixes their behavior. ([profiles](https://webassembly.github.io/spec/core/appendix/profiles.html#defined-profiles)) |
| Multiple memories | Permits multiple imported/defined/exported memories and adds memory indices to memory instructions and data segments. ([spec change history](https://webassembly.github.io/spec/core/appendix/changes.html#multiple-memories), [proposal source](https://github.com/WebAssembly/spec/tree/main/proposals/multi-memory)) | Moderate | No new value kind, but expands the instance memory model from one implicit memory to an indexed directory. Bounds, import aliasing, growth, bulk operations, codec state, and teardown must all be per-memory. |
| 64-bit address space | Allows `i64` address types for memories and tables, changing the operand types of their operations. ([spec change history](https://webassembly.github.io/spec/core/appendix/changes.html#bit-address-space), [proposal source](https://github.com/WebAssembly/spec/tree/main/proposals/memory64)) | Moderate–high | Expands bounds arithmetic, reservations, overflow handling, ABI lowering, and metadata. It does not itself add managed ownership, but a single truncation bug is security-relevant. |
| Typeful references | Generalizes reference types, adds subtyping, `call_ref`, null branches, non-defaultable-local tracking, and table initializers. ([spec change history](https://webassembly.github.io/spec/core/appendix/changes.html#typeful-references), [proposal source](https://github.com/WebAssembly/spec/tree/main/proposals/function-references)) | Moderate–high | Extends validation and runtime type identity across modules. Function references remain opaque store references, so exact function/type/store ownership matters even without WasmGC. ([Core 3 types](https://webassembly.github.io/spec/core/syntax/types.html#reference-types)) |
| Exception handling | Adds tags, exception references, `throw`, `throw_ref`, `try_table`, and unwinding. ([spec change history](https://webassembly.github.io/spec/core/appendix/changes.html#exception-handling), [proposal source](https://github.com/WebAssembly/spec/tree/main/proposals/exception-handling)) | High | Expands the control and runtime object model. Confidence requires payload rooting and correct unwinding through native, host, tail-call, and cross-instance boundaries, plus tag identity at link time. |
| Garbage collection | Adds recursive/sub types, managed structs and arrays, i31 values, casts/tests, allocation, mutation, and bulk array operations. ([spec change history](https://webassembly.github.io/spec/core/appendix/changes.html#garbage-collection), [proposal source](https://github.com/WebAssembly/spec/tree/main/proposals/gc)) | Highest | Introduces a managed heap, lifetime tracing, barriers, exact roots, richer subtype identity, and embedder ownership. The spec explicitly classifies aggregate heap types as dynamically allocated managed data. ([Core 3 heap types](https://webassembly.github.io/spec/core/syntax/types.html#heap-types)) |

Profiles are not version switches. The specification describes them as static,
coherent subsets, says the Full profile contains the complete language, and
says profiles are not intended for language versioning. Wago should therefore
keep feature admission separate from any future Full-versus-Deterministic
execution-policy choice.
([Core 3 profiles](https://webassembly.github.io/spec/core/appendix/profiles.html))

## Recommendation

For the compatibility default, enable the additive families that do not create
a new managed ownership model and for which Wago already has native backend and
official-suite coverage:

1. extended constant expressions;
2. tail calls;
3. relaxed SIMD when SIMD itself is supported by the current build/host;
4. multiple memories;
5. memory64/table64; and
6. typeful/function references.

Keep exception handling and WasmGC opt-in for now. Both are final Core 3
features, but they cross the most runtime ownership boundaries; WasmGC in
particular should not become a compatibility default until Wago's confidence
extends beyond the official conformance corpus to representative generated
programs, collection during every native/host/cross-instance boundary, and
stable public ownership behavior. Exception handling can graduate separately
once its exception-reference lifetime and host-boundary contract are considered
part of the stable default runtime model.

This selection must not weaken `SupportedFeatures()` as the executable
build/host authority. Unsupported architecture or bounds-mode combinations
should reject clearly rather than silently accepting a feature bit that cannot
execute.

## Official sources

- [WebAssembly Core Specification, Release 3.0](https://webassembly.github.io/spec/core/)
- [Release 3.0 change inventory](https://webassembly.github.io/spec/core/appendix/changes.html#release-3-0)
- [Implementation limitations and full-feature conformance rule](https://webassembly.github.io/spec/core/appendix/implementation.html)
- [Defined profiles](https://webassembly.github.io/spec/core/appendix/profiles.html#defined-profiles)
- [Official proposal snapshots integrated into the specification](https://github.com/WebAssembly/spec/tree/main/proposals)
