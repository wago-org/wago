# RV64 production parity tracker

Baseline: main `068208990b37d413141126325a85c32483b93895`, including the
shared compiler interface from #802 and checked register allocation from #800.
The accepted local checkpoint is `4898b7a1`. This tracker supersedes the narrower
checkpoint as the implementation goal; a bounded experimental subset is not
production parity. No remote publication is authorized.

| Area | Current implementation | Remaining production work |
| --- | --- | --- |
| Integer/control | RV64 integer arithmetic/comparisons, structured/indexed control, zero/multiple results, production compiler and experimental public binding/configured limits | Broader qualification and removal of development bounds |
| Calls | Defined same-instance direct/recursive integer calls, mixed/wide result vectors, production wrapper slots and cold traps | Stateful context transfer, imports, indirect/cross-instance calls, tail calls, call_ref |
| Production runtime | Linux/RV64 Engine/JobMemory bridge, central W^X/process-wide cache synchronization, framed normal/cold return, shared publication/cleanup ownership, experimental public instantiation | Host/guard/signal paths and native qualification |
| Memory/globals/bulk | True production JobMemory memory32, aliased integer global-cell directory, public active/passive descriptor/payload lifetime, bulk operations, full 4-GiB bounds/quota growth | Imported resources, extended initializers/start, multiple/shared/memory64 memories |
| Tables/references | Rejected before publication | Production descriptors, exact type identities, element/table lifecycle, externref and typed references, resource-root retention |
| FP | Reviewed fixed-width F/D encoder; FP modules rejected before publication | FCSR and ABI boundary audit, genuine scalar lowering/conversions and NaN/trap semantics |
| Host calls | Rejected before publication | Parked Go callbacks, wide slots, save/resume, panic/trap paths, re-entry, policies |
| SIMD | Rejected before publication | v128 two-slot representation, SWAR core/relaxed SIMD, spills/calls |
| GC/EH | Rejected before publication | Exact roots/frame/safepoint metadata, native views, exception tags/handlers/unwinding |
| Threads/atomics | Rejected before publication | Alignment/order, LR/SC/fences, wait/notify, shared-memory lifecycle |
| Cancellation/close | Cooperative production entry/guest-loop/bulk-loop polls, public context/deadline/gate/close tests, loaded-artifact cancellation and retained owners | Parked-host interruption, broader deferred-release qualification, RV64 Linux signal/context support |
| Public APIs/artifacts | Opt-in public compiler/instantiation, conservative configuration, image ownership, architecture3 trusted artifact round trip | Remove experimental gate after full feature parity; compiler-free/profile/TinyGo builds, generated facades and CLI qualification |
| Qualification | Focused host and QEMU regressions, existing-target checks | Official full-feature suites and native multicore cache/signal/lifecycle qualification; native performance remains unmeasured |

The integer multivalue/typed-control slice uses bounded argument/result arenas,
a fixed context header independent of module arity, and zero-normalized i32 cells.
It now also executes through the production wrapper and the opt-in public
compiler/instantiation envelope. Imported resource and owner-context contracts
remain prerequisites for table dispatch.

Independent runtime inventory found production contracts that subsequent slices
must reuse: 32-byte table entries, 40-byte canonical funcref descriptors with
owner context at +32, 32-byte import dispatch entries, exact structural type
identity before descriptor publication, InstanceContext identity independent of
linear memory identity, and retained resource roots after logical producer close.
See `src/core/runtime/limits.go`, `src/core/runtime/abi/basedata.go`,
`src/core/runtime/basedata.go`, and `src/wago/reference_store.go`.

Experimental trap codes and flat GuestContext layout differ from production.
Production stack/unreachable/divide-zero/overflow/memory trap codes are
13/1/9/10/3, and the trap/control area includes parked control and source location.
The stateless production envelope uses production trap codes and source layout;
the experimental GuestContext remains distinct and must not be advertised as
interchangeable with production state.

The shared production runtime now has a separately reviewed
[bridge checkpoint](riscv64-production-runtime-checkpoint.md), including active
Go GC and cross-thread grown-code execution under QEMU. Its wrapper and trap
contract is reused by the subsequent compiler and public integration.
The subsequent [publication ownership checkpoint](riscv64-publication-ownership-checkpoint.md)
retains failed raw mappings and public/private thunk cleanup through retries.
The [production compiler envelope checkpoint](riscv64-production-compiler-checkpoint.md)
first executed supported stateless modules through that runtime; unsupported
features fail closed before image creation.
The subsequent [production state checkpoint](riscv64-production-state-checkpoint.md)
adds defined memory32, globals and data/bulk lowering through real JobMemory
directories, without enabling public binding or imported-resource ownership.
The [cooperative cancellation checkpoint](riscv64-production-polls-checkpoint.md)
adds production polls and active interruption fixtures before public binding.
The [experimental public checkpoint](riscv64-public-checkpoint.md) binds the bounded
compiler to ordinary Wago APIs and artifact identity 3; the broader production
port remains unfinished.
The [scalar FP encoder checkpoint](riscv64-float-encoder-checkpoint.md) adds F/D
instructions and independent encoding/observer controls before admitting FP.
