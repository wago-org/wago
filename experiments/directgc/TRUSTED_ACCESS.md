# Trusted direct access: experimental attestation checklist

This document specifies the safety boundary for a separately attested native
read experiment. It is not evidence that the experiment has passed qualification
or outperforms another engine. Checked direct access remains the reference mode.
The initial scope is scalar `struct.get` on the existing final-type/layout slice
and `array.len`. Allocation, reference stores, casts, and array element bounds
remain unchanged in this first experiment.

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
