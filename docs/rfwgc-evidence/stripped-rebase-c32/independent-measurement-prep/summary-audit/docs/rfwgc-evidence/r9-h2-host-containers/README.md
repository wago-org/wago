# H2 Slice A: physical host-container authority

Production/test pin: `4bf395915fa2532b99bfec8f3a8d565cb6e4b217`.
Base: `67ef1f39e61dd61ee891bdefa5f8218a74aba806` (module-owned production
`501e9ab46c4f68090c7bbeb17a6ad14f4fadda6d`). This is Slice A qualification,
not combined H2 closure. B, the later parent race repair, and compiler repairs
are not included in this immutable source pin.

## Implementation and API readiness

Host-created extern tables and globals register one canonical authority in their
Runtime store, keyed by actual physical storage. A nil Instance in the frozen
store adapters resolves that authority; unknown, closed and foreign storage
reject before publication. No fabricated publishing Instance is used.

Public set, repeated-value replacement, clear and successful Close transact
checked roots and domain claims. Public ingress accepts only store-authenticated
public conversion tokens or ordinary externref tokens, never internal opaque
words. Host global reads preserve public-token translation after the publisher
physically retires. Host aliases share one receipt index; independent containers
own independent checked slots. Module-owned receipt behavior remains intact.

Each host checked slot has collector-local ownership metadata. Full joint marking
checks that its host authority is live and still canonical in the store directory;
closed or unbound metadata cannot act as an unconditional root. Active/callback
local collection retains conservative strength. Release removes slot ownership
metadata exactly with the checked-slot obligation. No permanent publisher retain
or opaque-to-compact cast is introduced.

Public mutation and Close acquire topology, the complete sorted domain set, then
native/domain mutation authority. Final claim journals drain only after releasing
all of those locks. Existing Close semantics remain: containers with live importer
or native leases cannot close. Clear removes the converted external edge while
leaving the container open. Successful Close releases receipts before unmapping.
Explicit Close defines host handle lifetime; there is no new automatic finalizer.

Migration preserves the canonical container and receipt/index identity while
changing the descriptor key transactionally. Rejected migration leaves the old
key and aliases intact. Production growth retains its existing reserved-capacity
policy; no relocation allocator or production relocation caller was added.

The four frozen A/B signatures and helper IDs 53–58/arities are unchanged. Parent
received A-first availability at this commit and subsequently authorized B.
Final combined qualification must use the integration owner's chosen newer pin.

## Tests and final gates

Six new top-level families live only in
`src/wago/gc_conversion_host_container_host_test.go`:
`TestR9H2HostContainerPublicGlobal`, `PublicTable`, `Close`, `Authority`,
`Migration`, and `Cycle` (each name has the full prefix). They cover publisher
physical retirement, exact public tokens, immutable/type rejection, ordinary/null
controls, independent storage and aliases, same-value/bounds rollback, foreign
storage/tokens, raw opaque rejection, closed/unbound slot filtering, physical-index
reuse, migration rollback, and final domain release.

Cycle fixtures use real compiled `extern.convert_any` and global publication to
form an aggregate/producer/provider cycle, then place its public token in a real
host container. The live host edge preserves the cycle; final clear or Close
reclaims it without another guest invocation. Default and actual region-shift-6
fixtures execute. Ownership assertions stop before subsequent collection/access
on failure. Sibling read-only source cross-check is in `sibling-readonly.json`.

`qualification.json` is the authoritative completed PASS verdict. Its checker
verified exact source pins,
unchanged driver hashes, exact command counts, exact nonzero test census, and
actual architecture probes/ELF machine checks. It preserves every skip name.

| Lane | ordinary | wagodebug | wago_regalloccheck |
| --- | --- | --- | --- |
| Focused ownership/conversion | 48 pass, 0 skips | 48 pass, 0 skips | 48 pass, 0 skips |
| Full Wago | 2503 pass, 44 skips | 2497 pass, 50 skips | 2503 pass, 44 skips |
| Scoped race | 48 pass, 0 skips | 48 pass, 0 skips | 48 pass, 0 skips |
| Backend/compiler | 998 pass, 552 skips | 996 pass, 554 skips | 1121 pass, 553 skips |
| Actual ARM64 QEMU | 40 pass, 0 skips | 40 pass, 0 skips | 40 pass, 0 skips |
| Actual RV64 QEMU controls | 45 pass, 0 skips | 45 pass, 0 skips | 45 pass, 0 skips |
| TinyGo compatibility | 5 pass, 0 skips | 5 pass, 0 skips | 5 pass, 0 skips |

Pass counts are top-level tests; skip counts include nested entries. Full/backend
skips are existing architecture, optional and diagnostic cases, never passes.
Cross ARM64/RV64 commands are compile-only, with no execution assertions.
ARM64 executes all six new A tests. RV64 opaque-host tests are build-excluded;
its 45 control tests qualify its separate compact representation only. TinyGo
uses the supported conversion smoke and build-excluded host stubs; it does not
qualify host opaque receipts or replace the complete foreign-stack lane.

The integrated driver has 18 commands; target driver 14 (two emulator checks,
six builds, six executions); TinyGo driver three. `scoped-4bf395915` separately
executes all 12 A-applicable adopted regex cells in all three modes, both ordinary
and race: 72 test executions plus exact list guards. A's source has no B tests.
The scoped export's 3989 `src` blobs exactly match the committed pin.

Reproduction uses unchanged `r9-h2-production/qualify-integrated.py`,
`qualify-targets.py`, and `qualify-tinygo.py`, with `--pin` set to the full SHA
above and fresh output directories. The target command supplies
`--qemu-package /tmp/qemu-user.deb`, verified SHA256
`ca6ede739327a20ae5a3498230c8a3a9a072c586e131bc6bcc1201eb1725e336`.
Each lane preserves commands, source archive/toolchain hashes, logs and ledgers.
`qualify-scoped.py` and `summarize.py` reproduce the per-cell and census checks.
Earlier A-only development overlays are documented in `DEVELOPMENT.md` and never
relabeled as final-pin qualification. No shared-tree B or parent dirty source was
used for these tests. Private cache/temp roots are owned by this slice.

## Limits and handoff

The parent's combined501e9ab findings remain independent: the lifetime-boundary
race was repaired later at `3b04e68376024c689ec651adf1817955ea75d707`; broad combined
race still needs the integration gate. No final combined, multi-slice H2,
staticcheck, conformance/corpus, performance, or reviewer acceptance is claimed.
RV64-used exception and receipt-proof fields remain intact for parent-owned lint
classification. No suppression or baseline waiver was added.

A source ownership was explicitly released to the parent after all final gates
and census checks completed. Only this evidence directory remained owned until
the evidence handoff.

Only nine owned source/test paths changed in the implementation commit; the
parent-approved collector metadata amendment is among them. Evidence stays in
this directory. Unrelated dirt/index, other slices, shared port log, support docs,
old evidence and combined-store-hook-gates were preserved. No subagents, Claude
reviews, benchmarks, timings, measurement lease, PR, push or thread closure.

READY TO MEASURE: branch `experiment/rfwgc-port`, production
`4bf395915fa2532b99bfec8f3a8d565cb6e4b217`. Sensitive paths are host extern table/global
registration, public store/Close admission, receipt acquisition/release, and
conditional root scanning. Proposed workloads: ordinary-token and converted-token
public replacement/clear, multiple physical containers/domains, final-cycle
release, and receipt-heavy collection. Measurement belongs to a different worker
under the global lease; no performance claim is made here.
