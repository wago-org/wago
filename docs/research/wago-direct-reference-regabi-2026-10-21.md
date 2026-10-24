# Direct reference calls through registers

## Scope

The candidate gives private direct-only local functions a register ABI when the
signature contains at least one reference, has at most one result, and fits
seven GP and eight FP argument registers. It excludes modules with tail calls
or exception handling. The independent `direct-reference-abi` switch controls
only this class; `reg-abi` remains the parent switch.

The existing module wrapper-reachability table is shared by caller and callee
admission. Addressable functions keep their old wrapper bodies. Every newly
admitted private function also retains an unused wrapper adapter so its runtime
descriptor cannot mislabel an internal entry. Exports, start
functions, `call_ref`, `call_indirect`, host calls and cross-instance links keep
their existing wrapper entries and descriptor identities. The internal direct
call relocation selects `InternalEntry`; wrapper calls select `Entry`.
There is no descriptor, artifact-format, collector, or handle-check change.

The existing typed-tail predicate cannot be used without narrowing it. It
admits two GP results, but ordinary mixed-register call lowering captures the
pair only when the first result is an integer Wasm type. Reference pairs stay
on the wrapper ABI. Eight GP arguments and vector signatures also stay on
their established path.

## Root and trap contract

Each direct call still consumes one source mask and records its native return
address. Hidden reference operands retain their exact root types and frame
slots. Reference arguments become callee locals. Collecting helper paths flush
operands, materialize lazy-zero root locals and spill dirty local pins before
publishing native RSP. The frame scanner retains compact-reference, frame-bound
and exact-offset checks. Register results are marked with `setStackGCRoot`
before later safepoints. Compact frame headers use the existing local-offset
remapping; retained adapters publish their return offsets.

Global pin writeback, argument trap order, result capture before local reloads,
and every existing runtime handle/type/extent check are retained.

## Static reach

A read-only scan of Dewdrop's saved `compiler.wat` on 2026-10-21 took 0.394 s.
The predicate explicitly required a reference in the parameter or result list.
It matched 6,578 of 7,012 local functions and 50,015 of 66,363 original direct
source call sites. Excluding exports, start and declared function-reference
identities leaves 6,458 functions and 49,954 original direct source sites. The
file contains no tail or EH instructions. These counts precede forwarding-call
retargeting and integer inlining. They are not dynamic counts or a performance
claim. An initial scan undercounted sites because it omitted numeric function
identifiers; these counts resolve both numeric indices and named functions.

## Scope correction from the collection regression

The first stats regression failed on unchanged main in 10.824 s: the direct
reference edge emitted zero register calls and one wrapper call. An earlier
fixture-encoder error was corrected before this mechanism failure was obtained.

The first broad reference-entry candidate passed direct calls, but failed the
hidden-root case through an indirect call under forced and Tiny collection,
both fresh and after codec reload. It returned 35 instead of 42. The native root
walker treats an adapter return as the end of the chain; a new adapter between
Wasm frames therefore hides outer roots. The first patch excludes every
wrapper-reachable target instead of changing that runtime contract. The runtime
fixture preserves this case through an addressable wrapper that calls a new
private register-ABI target. No root-walker checks are changed.

The host API audit found no public guest-function constructor by numeric index.
`ExportedFunc` resolves an export name and returns a handle with private fields.
`TableFunctionIndex` and `FuncRefMatchesFunction` only inspect identities.
`NewHostFuncRef` creates a host callback, not a private guest descriptor; raw
reference values still pass issued-token lookup. Tables expose no public
private-index setter. Compiler and codec publication freeze execution metadata,
so mutating the public export map cannot add a new callable private function.
A fresh/codec regression verifies this last boundary and identity preservation.
Custom instruction modules remain excluded through the existing conservative
all-functions-addressable set.

Source wrapper reachability and emitted adapter layout are separate tables.
The source table gates both call lowering and body ABI selection. The emitted
table includes the retained private adapters, so compact adapter trap-body
sharing sees every real layout boundary. A diagnostic compiler assertion
rejects a newly admitted body if it has no adapter.

## Real-compiler return-cache regression

The first checked real-compiler probe failed with the new switch on at function
4800, offset 0x1c3: a cached global-cell pointer in RDI survived an arm ending in
`global.get; ref.as_non_null; return`. A later call in the other arm reused RDI.
The single-register result path bypasses `flush`, which normally invalidates
that cache. A small regression reproduced the same check failure in function 0
at offset 0xe. Its real-red run took 16.523 s.

Invalidating only explicit returns exposed the same stale cache when native GC
helpers were emitted after a fall-through return (function 4818, offset 0xec).
The final fix retires the global-cell cache in shared single-result placement,
after capturing the result. It covers both exits and changes no runtime check.
The regression exercises a returning global, a later direct call, and a callee
with allocation and a fall-through global result. The focused checked matrix
then passed in 13.619 s, and the full compiler probe passed in 13.181 s including
Go build time (two compiler invocations completed within the 1.07 s test).

## Real-compiler mechanism probe

A temporary diagnostic test compiled the exact compiler input with SHA-256
`c64fcf044b72cb7356ad598a35670045931337cfa7a6bb9d5916cd304b23fba6`, eight
function workers, allocator checking, and `WAGO_EXPLAIN=1`. Only the new switch
changed. The test asserted exact roots and measured encoded artifact sections.
It was removed after the successful run; full dumps and command records remain
in the ignored local validation directory. Builds and other correctness suites
overlapped, so this probe supplies no timing comparison.

| Measure | Off | On |
| --- | ---: | ---: |
| Emitted wrapper calls | 51039 | 1085 |
| Emitted mixed-register calls | 45 | 49999 |
| Emitted integer-register calls | 15262 | 15262 |
| Native code bytes | 22260253 | 19400045 |
| Encoded metadata bytes | 3642258 | 3664997 |
| Complete artifact bytes | 25902527 | 23065058 |
| Exact root metadata bytes | 2291438 | 2314319 |
| Adapter count | 18 | 6476 |

The emitted reference-call delta is 49,954. This is a post-plan count on the
public `cd029409b` base; it precedes installation of PR 1008's forwarding pass.
It must not be presented as a combined-stack post-forwarder count. Both builds
retain exactly 25,794 safepoints, 68,809 callsites and a maximum of 77 roots.
Host, GC helper, import and reference-call counters are unchanged. Extra private
adapters explain the small metadata growth while code falls by 2,860,208 bytes.

## Validation plan and status

The strict stats test was added and run before the production change. It
expects the reference direct edge to use a register call and retains exact
source/native call accounting. Runtime fixtures cover live parameters, returned
references, hidden operands, forced collection, Tiny collection, public artifact
reload, start functions, indirect/reference calls and function identity.
Shape cases cover seven-GP and mixed arguments, a reference result and void;
eight-GP, pair-result and vector cases must keep working through wrappers.

The microbenchmarks use 8,192 direct calls per invocation for a reference
parameter with either an integer or reference result. Only the new switch
changes between samples. Parent and candidate full compiler runs must use the
same compiler/request bytes, fresh caches, CPU affinity, worker count and
output/root checks. Record every sample, compile time, code/metadata size and
peak RSS. Do not overlap these runs with builds or tests.

The corrected private-only candidate passed the focused checked/stats matrix
in 14.649 s (package execution 0.168 s). It includes host reentry through a
collecting callback, live private parameters and outer hidden roots, null and
i31 values, dirty pinned globals, and argument-versus-callee trap order. The
existing cross-instance, start, host-reentry and adapter-root tests also passed.
The normal micro binary built in 12.811 s; schema generation took 2.256 s.
Full compiler timings remain.
Command, exit and monotonic wall-time records are in the local ignored
`.tmp/direct-reference-abi/` directory.

The first full normal/checked passes retained visible setup failures: missing
PowerShell ICU configuration, missing pinned spec-interpreter environment, and
an intermittent `ETXTBSY` in plugin publication. The initial checked bench-module
suite passed in 36.434 s. The next normal full pass found only two expected
code-size golden changes in the type-subtyping products (405/797 to 377/769 and
377/918 to 362/903 code/codec bytes) and the nested installer dependency check.
The exact size goldens are updated; no behavior assertion is removed. The
installer requires the checkout's `go.work` to bind its parent-module dependency
to this source tree, so root suites use an explicit local `GOWORK` path.
Final suites also use `DOTNET_SYSTEM_GLOBALIZATION_INVARIANT=1`, `CGO_ENABLED=0`,
and the official interpreter and revision pinned to
`9d36019973201a19f9c9ebb0f10828b2fe2374aa`. Original failure logs remain intact.

Final validation is green:

| Command | Wall time | Result |
| --- | ---: | --- |
| `go test ./...` | 39.802 s | Pass |
| `go test -tags=wago_regalloccheck ./...` | 40.715 s | Pass |
| `(cd bench && go test -tags=wago_regalloccheck ./...)` | 29.024 s | Pass |

Root suites ran with this worktree's explicit `go.work`; the nested benchmark
module used `GOWORK=off`. The final root suites include the complete affected
packages after the earlier setup and plugin-publication failures. No assertion
or test was skipped to obtain these passes. The two root suites overlapped as
correctness work; their wall times are not performance comparisons. Formatting,
generated schema, and staged whitespace checks are clean. The micro fixture
does not access globals, so the later terminal-cache fix does not change its
generated call path; full compiler runtime qualification remains separate.

## Isolated micro result

On the AMD Ryzen 7 8845HS, CPU 15 and `GOMAXPROCS=1`, the normal test binary ran
five 200 ms samples per case in 5.002 s. Each invocation makes 8,192 direct
calls. Only `direct-reference-abi` changes; inlining is disabled in both cases.
Every case checks exact native roots and the expected numeric output. The first
launch failed before timing because its working directory did not contain the
relative package fixture path; the recorded successful run uses `src/wago`.

| Result | Switch | ns/invocation, all five samples | Median |
| --- | --- | --- | --- |
| Integer | Off | 32878, 32999, 33715, 33501, 33569 | 33501 |
| Integer | On | 28200, 28600, 28321, 28271, 28776 | 28321 |
| Reference | Off | 31992, 31573, 32051, 31597, 32116 | 31992 |
| Reference | On | 27160, 26952, 27130, 27426, 27089 | 27130 |

These medians decrease by 15.46% and 15.20%, respectively. All samples report
zero rounded Go allocations per invocation. Reported byte medians are 33 to 29
and 31 to 28; different iteration counts amortize collector storage differently,
so this is not evidence of a memory reduction. Samples run in grouped Go
benchmark order, not ABBA order. This is a synthetic mechanism result, not a
full compiler performance claim.


## Full compiler qualification on the combined stack

Three serial balanced rounds compare the control, this independent candidate,
a separate candidate and V8. Each round changes native call/decoder code only;
all Wago variants share stack d06313bc3 plus the same new refDesc physical-span
safety checks and abstract-array byte decoder. They do not include the held
collector recycling experiments. The combined refDesc implementation retains
its existing pointer/bytes fast path and adds the same span checks there.

All engines use the same compiler/request bytes, CPU affinity12,14 and
GOMAXPROCS=2. Each sample starts a fresh native cache and misses the transform
result cache. No task-owned build/test overlaps. Order is control/ABI/byte/V8,
byte/ABI/control/V8, ABI/control/byte/V8. This is a shared-host observation.

| Variant | Wall seconds | Median seconds |
| --- | --- | ---: |
| Control | 42.648727, 42.446735, 42.375415 | 42.446735 |
| This candidate | 38.815080, 38.532937, 38.858979 | 38.815080 |
| V8 | 4.058387, 3.950350, 4.008502 | 4.008502 |

This independent candidate takes 8.56% less wall
time than control. It still takes 9.68 times V8; this does
not establish parity. All12 samples have identical output Wasm/cache hashes,
validated Wasm and main42. Every Wago artifact has required and exact native
GC root admission. Full identities, commands, samples, output hashes and
admission sizes are saved in [direct-reference-regabi-full-compiler](direct-reference-regabi-full-compiler/summary.json).
No combined-gain or memory-gain claim is made from these independent samples.
