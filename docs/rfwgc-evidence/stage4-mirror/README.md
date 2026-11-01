# Stage 4 landed source mirror disposition

The old-space source dependency is now available. Slipstream's landed fix expands
bounded demand-paged mature capacity and derives metadata consistently; it does
not replace conservative destination admission. Adapt its invariants to Wago's
stable handles and transactional backing. Do not double Wago capacity by analogy.
This completes the mirror disposition through the pin below, not implementation
or qualification of the full port.

## Pins and scope

Research date: 2026-11-01. Existing Wago worktree:
`/home/dev/.t3/worktrees/wago/experiment-rfwgc-port`, branch
`experiment/rfwgc-port`. Starting head:
`94846b133a4c851ec5110d688453c3e9a2bf466f`; production reference:
`5b31c81ec0c74695a90bc8d36754b92845b59f14`.

Source range, exclusive base and inclusive tip:
`506b08d3d3fa3c8e8b1ce9ac0cb4f30a2463cdc3..82d546679bab4887acfab056f6ad773a969b1e03`.
Local Slipstream `origin/master` equals that requested fresh pin. All sibling
source reads use git objects, never the dirty checkout; no fetch was needed.
[Ancestry](pins.json) records full commit identities, parents and ancestor checks.
[Range inventory](landed-range.txt) includes every commit and changed path.
Commit topology, rather than author timestamps, establishes landing order.

Read the resumption brief, launch addendum, Wago AGENTS.md, Stage 3 status and
mirror/runtime/integration/review handoffs. This directory alone is owned.
Earlier handoffs saying old-space is absent remain accurate at their historical
pin; this assessment supersedes that dependency status. The existing full-suite
worker on immutable `d546a1af4` is not duplicated. The babysitter completion and
owner's highest-priority continuation order are accepted as task context.

## Exhaustive commit disposition

| Landed commit | Source change and Wago disposition |
| --- | --- |
| `ccc6512de266916d2e6aabab09c50ba465019953` | Mature expansion, derived metadata, generated descriptor/card offsets, expanded-space regression. **Adapt** capacity/layout/admission invariants; inspect [mature policy](mature-policy.md). The actual implementation ancestor, not the documentation tip. |
| `bb0900efa2d5db8d037f3990ce587a4127a8c9db` | Production diagnostic state, arithmetic/layout guards, optional evacuation admission, failure reporting and regressions. **Adapt** transactional refusal and retry requirements. Despite its `test(gc)` subject this changes runtime code. |
| `7822fc8c42b0dcadd9605d990e6c060440fbe9ad` | Return guest exit status through invocation and initialization to release Store and close collector before process exit. **Adapt** teardown semantics and exit tests; not a general rewrite of every trap/error exit. |
| `0c9da13db44341709314a309d2a742b168ce4f2c` | Disabled Dewdrop transform worker import catalog/dispatch and host contract inventory. Capacity returns zero; job/channel operations trap. **Keep as corpus compatibility context**, not evidence for collector parallelism. Initial packed-byte parameter checking is revised by the later Facet commit. No collector pool transplant. |
| `03cdd75bb2415efec2a69e4cadd668e61eafc4cb` | MoonBit import modules, instance-owned tagged externref records, string/byte/list/readers/builders and confined filesystem host implementation. **Adapt ownership/root distinctions**, not a requirement to clone the complete filesystem API. See [host and future stages](host-and-next-stages.md). |
| `95c7bace2b60602fc98cbaec3ef23d387e53892a` | Facet numeric/nested GC buffers, allocating typed results, native/legacy bridge, filesystem directories/links and resource cleanup, MoonBit aggregate backing bound/disposal, catalogs/generator, regression and load-verification tools. **Adapt borrowed-view and result-rooting requirements**. Filesystem confinement, UTF conversion and network denial are host/corpus compatibility concerns, not GC allocation policy. |
| `5cfc8f461c3a2a6535221c17a6210ea5af0ae229` | Explicit decimal page-aligned guest stack option, 1–256 MiB, applied separately to operand, local and native stack allocations. **Carry bounded configuration and root-map validation requirements**; no blanket Wago default increase. |
| `2c688c20f8eee9543d16e45ea626748037aa1509` | Only `docs/function-facts-coverage.json`. Structural comparison records consumer source-line updates in [facts-fingerprint.json](facts-fingerprint.json). **Evidence refresh only**: no new facts implementation, proof or execution qualification. |
| `82d546679bab4887acfab056f6ad773a969b1e03` | Removes one trailing blank line in host contracts. **No runtime port action**; this is the mirror cursor, not the old-space implementation. |

The host source/test patch preserves cumulative changes after close-before-exit;
the three collector/exit patches preserve exact individual changes. These are
read-only evidence, not patches to apply to Wago. The full path inventory includes
contract/catalog edits omitted from the cumulative source archive.

## Exclusions and remaining gates

Both type-cap implementation `059d10f170c0a513cbcf65ab2956f3dff717a892` and report
`01303964bf52d2f3c86f556480cb07cf5ec35165` fail ancestor checks against the new tip.
C1/C2 remains excluded; there is no new owner-authorized landed status in this
range. Growth safety remains an independent Wago invariant, but parked code and
its capacity numbers are not imported. All tabled Wago GC PRs remain excluded.
Dispatch trim `cc48b98eaa147966a2a9ca8ecbc7b48bb51fef6d` and wake/join
`cf05aaf8149c9b0f38a74b57454f98254e8f9fc6` are ancestors; Stage 3's protocol
assessment still applies.

Read-only Starshine cross-check uses
`e075c47e61b01ab9e4fd0ae4a7ca47b755e3fca1` in
`/home/dev/Projects/starshine-master-base`, including
`docs/wiki/wasm-gc-core-boundary.md`. Its model/codec/validator/authoring surfaces
provide fixture constraints, not a native collector implementation to transplant.
No new verified defect is alleged in Wago or either sibling; the requirements
are prospective qualifications, not defect triage. Any later verified defect
still needs its own sibling cross-check before triage.

The owner's old-space landing waiver stopped the measurement chain. Landing
establishes the code to mirror, not a positive, neutral or negative timing verdict.
No benchmarks, timings or tests were run here. Existing three-mode correctness
results retain their exact prior scope; documentation needs no rerun. This change
cannot affect shipped performance because only evidence files are changed and
none are consumed by production builds. Future implementation remains subject to
ordinary, wagodebug and wago_regalloccheck gates, then independent measurement
under the global lease after tests drain. Review fan of three Daybreak plus one
Claude remains a mandatory later final gate, with consolidated triage and zero
unresolved findings before a ready PR. No review fan, PR or push occurs here.

Validation: git-object ancestry and exact range inventory, parsed structural
facts comparison, evidence hashes, owned-document whitespace/link checks, and
owned-only staging. The report commit is named in the handoff, avoiding a
self-referential SHA. Shared corpus scripts, port log and spec submodules are
preserved.
