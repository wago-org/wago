# Stripped rebased PR 1048

Old published head: b6260d2d7c993071e03aeb081429e91ac64503dc.
New head: c32ebece3674cc2a4c79099753f3238202c7226d.
Base main: 90f2b57dafa7f8c3cc7e3aca38b91148f15e1d1e.

329 commits rebased; no merge commits in the port range. Unpushed merge 0c4f033bb was retained only as a local comparison. Full evidence-bearing rebased head e65ff9373 was pushed to campaign/rfwgc-port-record before stripping. Archive 59e13cd798d090406e2dd037a738097fe8b0cea2 then added dirty-work custody and reconciliation receipts, and was remotely verified before removal. Dirty docs are also retained in a named stash; dirty spec gitlinks and the stray ARM64 artifact were not changed.

Conflict resolutions:
- AMD64 driver.go: lifetime replay before tightened main reduction eligibility; preserve main fresh-struct mode guard. EH alias prerequisites already on main remain.
- AMD64 control.go: decode global.set index for main selective decoder; retain extern-store helper boundary.
- AMD64 hints.go: keep main extracted scanExtendedHint and scalar-memory fast path; keep extern global/table/bulk call classification. Added durable TestExternStoreColdDecodePreservesCallBoundaries.
- No collector-core design conflict occurred. Main Tiny gray-object guard and ARM64 router repair retained.

Stripping changes no production source. Removes 17,904 tracked campaign paperwork files and campaign-only CI dispatches/policy tests. Normal main CI plus two TinyGo foreign-stack checks remain. No references to deleted paperwork in tracked .github/src/tests/scripts/go.mod. Tests using removed paperwork: none found in the relevant suites.

Fresh immutable stripped-head checks are in final-c32; CI-policy/planner all modes and explicit-config actionlint are in ci. Initial actionlint without explicit repository config failed on known runner labels; retained separately. ARM/RV checks are compile-only. These checks do not close the preexisting native checked-race PDFium timeout, final CI, or laptop benchmark proof.

Diff against main: 266 files, 24761 insertions, 570 deletions. GitHub verification and initial CI receipts follow publication. No benchmarks performed by integration owner.
