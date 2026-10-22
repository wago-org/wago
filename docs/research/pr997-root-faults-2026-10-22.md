# PR 997: foreign native frame roots

This repair starts at `d329e9df9f09488f8906dfa3cee7f396574851d0`.
The unused-funcref-table controls already pass there. The earlier PR description
and merge note incorrectly carried their old failure forward without a new run.
The A-to-B-to-A lifetime control still fails: forced collection returns zero
instead of the live outer object's value, 73, under both call ABIs.

## Cause and repair

A scalar call says that no collector object crosses that one signature. It says
nothing about older native frames. Stopping at a foreign collector therefore
loses roots when an older frame belongs to the collecting domain.

AMD64 and ARM64 now follow the foreign frame's exact return map. They validate
physical stack extents, return PCs, register adapters, the 64-byte cross-domain
context record, and its exact instance identity before proceeding. They skip
foreign collector slots and resume scanning when the frame owner belongs to the
collecting heap. The original owner still selects the collector and lookup store.
RV64 keeps its existing physical adapter and collector boundary rules.

Numeric relays retain zero-root return maps. They do not gain a collector ABI or
an object execution product. Validation rejects compact root slots, allocation
safepoints, and collector-bearing function bodies in these maps. Existing codec
records carry the maps; no root-record prefix, machine ABI, or format version
changes. Inert unused collector function signatures survive saved-code reload
only after this complete zero-root metadata check.

Return-map planning must preserve numeric Go dispatch. An explicit compile-only
`UnwindOnly` flag keeps that host fast path. Its host and native machine branches
consume one logical call mask and publish their separate return PCs. The ARM64
native-leaf shortcut is disabled at these mapped callsites because it has no
return-map publication. The new flag occupies existing plan padding.

## Retained cross-store owners

A second failing control leaves A's store through a numeric import in B's
Runtime and returns to A's collector. Local store lookup alone loses A's root.
The cold fallback follows retained immutable function imports across stores.
It deduplicates owners, uses 32 inline entries, and grows scratch for wider graphs.
It takes no foreign store lock and does not use a scalar signature as a stop proof.

The lifetime ordering is essential. `referenceLifetime.finalize` announces
`referenceLifetimeResourcesReleased` to the local store before `releaseResources`
detaches function imports or releases native code. Holding that store's mutex
therefore keeps each eligible registration's entire imported code/context graph
physically retained, including logically closed producers. No callbacks or
resource-root releases run from the lookup. Tests traverse 40 instances sharing
one code image and require exact saved-context selection after producer close.

## Host suspension

An added host control also reproduced a lock wait: a numeric foreign relay
entered Go while the older public caller still owned its collector lease.
The public root now selects suspension whenever it owns local, imported, or
dynamic collector domains. AMD64/ARM64 park the numeric relay's mapped frame
when that public root can collect. Collection finds parked chains through the
same retained import graph, keeps physical frame ownership separate from target
collector ownership, and scans only the target domain's slots.

A global atomic count bypasses foreign parked-chain discovery when no mapped
GC host activation is parked. Publication precedes lease release; removal
follows native reacquisition. Plain numeric host calls do not create GC
activations. Existing bounded control snapshots and physical stack bounds remain.
No per-instance or public-state layout grows in this repair. A numeric relay's
first GC-capable host callback can now allocate its lazy public GC state
(23,440 bytes of state on AMD64). That extra setup cost belongs to a previously
failing path and is outside the unchanged ordinary-control allocation counts.

## Regression coverage

The new tests require the live outer value and an actual forced collection:

- Direct, unused-table, indirect and reference scalar callers, with and without
  objects or unused collector signatures; both ABIs and saved-code reload.
- An allocating callee with no module-local callsites.
- A-to-B-to-A through a mutable function table, with incompatible collectors or
  a collector-free relay, under throughput and Tiny collection.
- Different-store immutable import chains of depth one and 40, shared code,
  both ABIs, saved code, and logically closed retained producers.
- Host-triggered collection through same-store and different-store numeric relays.
- Rejection of forged collector-free root slots and allocation safepoints.

The existing owner tests now check exact code/context identity across domains;
they no longer assert the unsafe scalar early-stop rule. Stack bounds, wrong
contexts, retained owners, adapter limits, root kinds, and ordinary numeric host
reentry remain covered by the existing suites.

## Evidence and limits

Raw red/green logs, commands, wait4 timing/RSS records, paired benchmark samples,
and the final source patch are retained with the Dewdrop research evidence.
The first full run exposed numeric host-route failures; they remain recorded.
The host lock-wait reproduction was stopped with SIGQUIT after its blocked
invocation-gate stack was captured, rather than left running indefinitely.

All suites use Go 1.27.1 with GOMAXPROCS=2. Native package concurrency is two;
cross-target compilation uses one. Times include builds unless stated otherwise.
The pinned spec interpreter/corpus revision is
`9d36019973201a19f9c9ebb0f10828b2fe2374aa`.

| Check | Wall seconds | Peak RSS KiB |
| --- | ---: | ---: |
| Complete ordinary root suite | 145.454 | 1,176,008 |
| Complete register-check root suite | 150.094 | 3,506,104 |
| Complete register-check benchmark-module suite | 32.911 | 815,964 |
| Final ordinary Wago binary, after host/owner guards | 22.483 | 199,732 |
| Final checked Wago and runtime-concurrency suites | 48.929 | 3,607,224 |
| ARM64 checked root/host/codec cases, three repetitions | 22.264 | 2,738,100 |
| RV64 checked parked-root/codec/plugin controls | 33.130 | 4,820,072 |

The broad suites precede two equivalent pointer-identity fast guards. Final
complete Wago and ARM64 checks above include both those guards and the final
plain-numeric host guard. RV64 checks follow the pointer guards; the last host
guard changes only AMD64/ARM64 files. The ordinary Wago binary row excludes its
separately measured build. The plain-numeric host guard uses actual collector
ownership rather than walking compile-product metadata.
These activities above 30 seconds remain build/test performance bugs under the
project budget; passing results do not excuse that cost.

An additional ARM64 QEMU concurrency run passes all root and host-reentry cases
three times, but fails each shared-memory wait-marker case after its original
15-second limit. The unchanged `d329e9df9` baseline fails the same isolated seed
with the same marker error (24.509 seconds including build). The module has no
function imports, tables, or GC objects, so it does not select the new return-map
path. This is an existing emulation limitation/fault, not a green gate. Its raw
failure remains in the evidence. No native ARM64 thread result is claimed, and
no timeout or assertion was relaxed.

Paired AMD64 cost controls alternate baseline/candidate, three 200 ms samples
per case. Both baseline and candidate semantic oracles pass (`wrong/op=0`). The
initial candidate added about 16% to deep mapped walks. Pointer-identity guards
remove repeated collector comparisons when ownership stays fixed.

| Final mapped/root control, median ns/op | Baseline | Candidate |
| --- | ---: | ---: |
| One frame | 13.70 | 14.17 |
| 64 frames | 496.4 | 513.7 |
| 4,098 frames | 31,939 | 32,021 |
| Local extern conversion, op 26 | 3,238 | 3,229 |
| Local extern conversion, op 27 | 3,252 | 3,272 |
| Eight foreign locals / 256 identities | 18,593 | 16,867 |

Mapped walking stays at zero allocations. Local conversion remains 224 B and
12 allocations per operation; foreign-root controls keep their previous
168–169 B and nine allocations. These short samples establish cost bounds,
not a broad speedup claim. The old code is not a valid timing alternative for
the new A-to-B-to-A workload because it loses roots.

The numeric relay compile control costs 12.071 → 13.426 microseconds, 20,146 →
20,665 B, and 91 → 102 allocations. Its native code remains 437 bytes; its
serialized return-map estimate grows from zero to 33 bytes. Three strict
function-subtyping fixtures keep native code unchanged while their complete
codec sizes grow by 253, 37, and 106 bytes. The exact size checks now require
those measured values. The initial host-latency controls showed a roughly 4.9%
aggregate increase; the final ownership guard removes unnecessary compile-product
lookups from collector-free host calls. Final paired host samples use 64 callbacks in the same serial Caller loop:
without linear memory, 11,855 → 11,458 ns; with four memories,
11,885 → 11,624 ns. Both retain zero allocations. These are three-sample
medians for the two stated controls, not a whole-runtime speed guarantee.

The full `just lint` gate passes with verified Staticcheck 2026.2.1 across its
standard, runtime, profiling and diagnostics profiles: 196.622 seconds,
3,628,364 KiB peak RSS. Generation, both vet commands, formatting and all four
script suites are included. Documentation validates 86 tracked Markdown files
in 0.059 seconds (27,568 KiB RSS).
Known retained code without a return map produces an explicit recompile error.
Unsupported custom lowerings retain their previous admission limits. This repair
does not qualify a broader arbitrary native ABI or claim the four-second target.
The separate `fd59548a` scalar-boundary prototype is not integrated.
The PR remains draft pending hosted qualification and review; no independent
source approval is claimed.
