REBASE PUSH COMPLETE

New head: `c32ebece3674cc2a4c79099753f3238202c7226d`.
Old published head: `b6260d2d7c993071e03aeb081429e91ac64503dc`.
Rebased onto main `90f2b57dafa7f8c3cc7e3aca38b91148f15e1d1e`; no merge commits in the port range.

The PR now has **266 changed files, 24,761 insertions, 570 deletions**—a few hundred source/test/support files. Campaign paperwork and campaign-only workflow dispatch jobs were removed. Normal TinyGo foreign-stack regression checks remain.

The full evidence-bearing head was pushed to `campaign/rfwgc-port-record` before removal. The archive also preserves dirty scripts/log custody and exact qualification receipts; no evidence was discarded. All PR evidence links now point to that archive.

Conflict resolutions preserve main's cold-compilation decoder/hint fast paths and tightened reduction eligibility while retaining lifetime replay, fresh-struct mode guards, and extern global/table/bulk helper call boundaries. Added `TestExternStoreColdDecodePreservesCallBoundaries`. No collector-core design ambiguity occurred.

Fresh immutable stripped-head gates: all 18 passed across ordinary, wagodebug, and wago_regalloccheck (focused GC/rooting, full Wago, scoped race, compiler/encoder/collector suites, ARM64/RV64 cross-builds). CI-policy/planner pass all three modes; actionlint passes with the repository's explicit configuration. Cross-builds are not target execution.

This is not final CI or merge approval. The earlier native checked-race PDFium timeout remains a separate open qualification item. Joshua directed laptop benchmark proof to start on this new head after push, under independent measurement, global lease, quiet-laptop and thermal controls. No fresh benchmark result is claimed yet.
