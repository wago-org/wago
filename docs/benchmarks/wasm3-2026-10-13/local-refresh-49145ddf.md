# Complete retained snapshot visible locally

At 2026-10-14 00:59:58 UTC, published the complete retained `49145ddf` Wago
snapshot to the local wasm.fyi API only, retaining the pinned w2c2-clang peer.
The deferred publisher honored the requested approximately two-hour refresh
interval. All 266 immutable captures / 1,064 phase rows were checked against
source identity, artifact/contract hashes, latency and resource measurements.
SQLite integrity passed, with zero foreign-key violations; 575,121 historical
observations were preserved.

Chrome loaded the user's exact URL:
`http://localhost:8788/features/?avg=mean&m=m2&rt=G%2Cengine%3Aw2c2-clang&view=perf`.
There were no page errors and 22 numeric Wago feature rows. The MVP Tables row
showed approximately 93.5 us versus 187 us for the peer, confirming that the new
complete source snapshot, rather than the older table measurement, was selected.
The browser also observed a numeric Threads row. A full-page screenshot and
structured browser/database audits are saved privately under
`.validation/wasm3-parity/local-refresh-49145ddf/`.

This publication includes the affine-table optimization. Newer fixed-shared
callback and native-import context optimizations require their own complete
source snapshot before publication. The next routine local refresh is due no
earlier than 2026-10-14 02:59:58 UTC. No production deployment or push occurred.
