# Trusted direct access: experimental attestation checklist

This document specifies the safety boundary for a separately attested native
read experiment. It is not evidence that the experiment has passed qualification
or outperforms another engine. Checked direct access remains the reference mode.
The initial scope is scalar `struct.get` on the existing final-type/layout slice
and `array.len`. Allocation, casts, and array element bounds remain checked. The policies below
separate the read experiment from the subsequent reference-store experiment.

## What the compiler may rely on

A non-null reference reaching an admitted typed object access denotes a live
object in the owning direct collector arena, with a layout compatible with the
validated Wasm operand type. This is a runtime/compiler invariant, not an escape
analysis result. Objects still allocate, survive, move, and die normally.

Given that invariant, the initial experiment may replace repeated start-map,
active-range, header-size, canonical-type, and ABI checks with a null check and a
fresh arena-relative field load. `array.len` reads the exact header Aux length.
It must preserve the null trap and its instruction provenance. It must not use
this reasoning to remove logical array bounds or dynamic cast/ref.test checks.

## Required admission and ownership

- A fresh compiler issues a separate private trusted receipt. The checked ABI
  version alone, a build tag alone, and caller configuration are not proof.
- The receipt follows the private execution snapshot, not mutable public
  compatibility metadata. Decoded/cache-loaded or hand-built artifacts do not
  acquire it. It must not be serialized or reconstructed from caller fields.
- Trusted native code is admitted only with the matching direct ABI and an owned
  exact-root Runtime collector domain. A handle collector must reject it.
- Keep the existing direct admission restrictions: no imported functions,
  globals or memory, tables/elements, exception handling, managed external
  references, custom instructions, custom collectors, or runtime plugins.
  Public exported function arguments remain numeric. Do not silently remove
  exported retained results or globals to fit a benchmark.
- Canonical descriptors and local-to-canonical mappings are immutable owned
  copies. Validated module layouts agree with all allocating helpers and native
  constructors, including header, padding, and exact array length semantics.
- Execution, collection, close, and reference publication obey the existing
  collector-domain serialization and lifetime rules. Arena/view owners remain
  alive for every native execution interval.

## Reference ingress

| Boundary | Required rule |
| --- | --- |
| Numeric Invoke/InvokeValues arguments | Cannot introduce raw reference bits; reference parameters remain outside admission. |
| Globals | Raw reference-bit setters reject; typed publication checks token owner/domain and declared assignability. |
| Retained results and tokens | Resolve authoritative mutable root slots after owner/token validation; cached issuance identity is not the current location. |
| Foreign graph cloning | Validate every reconstructed field/element edge for target type, subtype, nullability and i31 compatibility before payload publication. Keep the clone-edge fix and its adversarial tests. |
| GuestStorage numeric bytes | Reference arrays are excluded, slices exclude headers/padding, and borrow lifetime prohibits moving collection/reentry. There is no admitted raw reference-valued struct byte writer. |
| Plugins/imports/custom collectors | Not admitted, even if another API normally supports them. |
| Low-level collector/Payload APIs | Trusted implementation interfaces, not safe external ingress. Supplying or exposing raw mutable reference payloads would invalidate this attestation. |

The clone rule matters even when the source graph contains live references:
liveness alone does not establish that a reference is assignable to the target
field. A custom source collector may supply a live but wrong-typed edge.

## Moving-root contract

Every live reference across an allocating helper must have an exact mutable root
slot. Collection rewrites frame slots, owned globals, retained tokens and
constructor scratch, and resumed code reloads the rewritten values. Merely
visiting a copied reference value is insufficient. Every constructor rereads
rooted reference initializers after any allocation that can collect.

A native scalar read does not retain an address certificate across helpers. It
reloads the current arena base and uses the current compact offset. Do not add
cached object pointers, speculative unrooted references, or extra safepoints
inside allocation/store publication sequences as part of this reduction.

## Boundary of the claim

This mode does not defend against arbitrary unsafe Go memory writes, manually
corrupted native ABI structures, or misuse of low-level collector payload APIs.
Checked mode retains malformed-metadata guards and their tests. Do not delete
those tests or redefine corruption as a valid Wasm input to make trusted mode
pass. Supported public APIs must still preserve Wasm memory/type safety.

## Qualification before performance conclusions

1. Reject wrong collector/configuration, missing or stale receipts, decoded
   artifacts, custom plugins/collectors and unsupported imported/reference
   entrypoints. Mutating public metadata must not create authority.
2. Exercise null traps with exact instruction location, scalar widths and signed
   packed reads within the admitted layout slice, and exact array lengths.
3. Exercise forced collection, rewritten local/global/result roots, constructor
   scratch, and repeated invocations after relocation.
4. Retain adversarial clone-edge tests and valid subtype/i31/null cases; retain
   token ownership and cleared-root tests.
5. Run checked-mode corruption tests independently, plus broad default and TinyGo
   qualification. The opt-in receipt must not change default semantics.
6. Compare fresh-process benchmarks with matching artifacts, outputs, allocation
   and collection counts. Report code size, compile cost and regressions as well
   as latency. No V8 parity claim follows from eliminating checks alone.

Any later allocation/store reduction needs its own review. In particular, native
allocation must retain the policy capability, overflow-safe capacity check,
complete initialization/publication, exact accounting and rooted fallback.
Reference stores must retain logical bounds and the remembered-parent barrier;
a readonly dirty bit is a certificate of prior queue/overflow registration, not
a bit the JIT may set independently.

## First implementation checkpoint

The opt-in `wago_gc_direct_trusted` compiler mode implements only the read
reduction above. At `01c6c35`, its applicable `TestDirectGC*` and
`TestTrustedDirect*` runtime tests pass with `wago_regalloccheck`, including
unmodified retained workloads, moving roots, null provenance, receipt/private
snapshot checks, and forged/stale/wrong-type/wrong-domain global token rejection.
The dedicated trusted clone-ingress test rejects wrong-typed live source edges.
Unfiltered default Wago, native/checked GC, AMD64 compiler and direct substrate
tests also pass. `Compiled` footprint remains unchanged.

This is not an unfiltered trusted-mode pass: ordinary handle/Tiny-specific tests
cannot instantiate trusted artifacts, and checked-mode tests that deliberately
corrupt private Go metadata do not apply to this attestation. They remain in the
default/checked test sets. Subsequent matched read measurements show mixed results,
not a general speedup; see the direct-bridge evidence.

## Attestation policies

- Policy 1, `wago_gc_direct_trusted`: only the scalar struct-read and array-length
  resolver reduction described above. Reference stores remain checked.
- Policy 2, `wago_gc_direct_trusted_store`: includes policy 1 and reduces the
  reference-store resolver only for the existing final array of nullable final
  struct references. Static operand typing plus checked ingress establish the
  parent layout and null-or-compatible child; no child object dereference is
  needed to copy its compact reference.

Policy 2 retains parent null checking before reading Aux, unsigned logical
index bounds, zero-extended 64-bit scaled address arithmetic, and the current
nursery membership test. An old parent still requires an in-range readonly dirty
certificate equal to one; a missing certificate takes the entire checked helper
before payload mutation. The JIT never sets dirty bits or appends queue entries.
All guards precede the single 32-bit payload store. No helper or safepoint
intervenes between address derivation and that store, and helper fallback does
not resume using the pre-helper address. Native allocation remains checked in
both policies.

Public token/global/clone ingress negatives remain required in policy 2. Tests
that deliberately write a wrong-typed child directly into private native state
exercise checked reference stores (including policy 1), not policy 2's contract.
They must remain available rather than being removed.

The independent `wago_gc_direct_scalar_arrays` experiment always uses the fully
checked object resolver for numeric array get/set, even when combined with
policy 1 or 2. It retains logical index and selected-element physical extent
checks, admits only final numeric layouts up to eight bytes, and does not inherit
the trusted struct-read resolver through tag composition.
