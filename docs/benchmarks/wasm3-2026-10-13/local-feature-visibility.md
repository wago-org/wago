# Complete local feature snapshot visibility

The local Features page selected the newest qualified Wago source revision,
`6b46132b932cfc0fd4d526469f2ef46834248673`. Publishing five focused fill/copy
captures from that revision hid the earlier complete `d9985d87` suite: the
page intentionally selects one source identity per engine instead of mixing
revisions. Its machine filter `m=m2` correctly selected Apple M4 Max.

Collected all 118 feature workloads for the retained `6b46132b` revision with
the frozen SDK on Apple M4 Max, under the shared measurement lock. Each capture
includes compile, instantiate, first-call, and steady phases, three launches
with five samples per successful phase, RSS and code measurements. All cases
completed with zero collector errors or resource failures; 107 steady cases
passed and 11 were unsupported. The provisional index-mixer optimization was
not published.

Published this complete snapshot only to the local API on port 8789. The local
audit verified 118 receipts and all 472 phase measurements against SQLite,
including source, engine, contract, timestamp, latency, RSS, code bytes, and
sample count. All 575121 historical observations remained intact; SQLite
quick_check passed and foreign_key_check returned no violations.

Opened the exact reported URL in headless Google Chrome via Playwright:
`http://localhost:8788/features/?avg=mean&m=m2&rt=G%2Cengine%3Aw2c2-clang&view=perf`.
The rendered table has Wago and w2c2-clang columns, 22 numeric Wago feature rows,
and zero browser errors. Numeric instructions show approximately 61 us for
Wago and 77.4 us for w2c2-clang; these are displayed arithmetic means, not a
claim of overall parity. Unsupported and compile-only rows retain their
appropriate statuses. Visually inspected the screenshot as well.

Private evidence: `.validation/wasm3-parity/full-feature-6b46132b`,
`.validation/wasm3-parity/local-feature-visibility-6b46132b`, and
`.validation/wasm3-parity/ui-verification/after.{json,png,txt}`.
Publication and audit completed 2026-10-13 at 22:58:58 UTC.

Future local refreshes must finish the full intended feature corpus for a
newer source identity before publishing focused results from that identity.
Keep focused experiments private until that complete snapshot is ready.
No frontend aggregation logic was changed and nothing was deployed to production.
