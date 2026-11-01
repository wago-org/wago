# TinyGo serial evacuation contract

This slice separates compiler capability from collector profile. ProfileTiny is
still the fixed-block incremental nonmoving collector; the TinyGo compiler can
run ProfileThroughput. MatureEvacuation remains explicit, default false, and
requires fixed region Eden, ProfileThroughput and collection enabled. Workers
zero and one use one coordinator; TinyGo Workers greater than one remain errors.
A TinyGo threads scheduler is not support for the collector's helper pool.

The shared mature_evacuation.go implementation selects physical old extents,
excludes LOS and source blocks, proves bounded run credit, checkpoints allocator
metadata, reserves destinations, and copies before stable-handle publication.
The TinyGo stub returns false from copyMatureParallel so the coordinator executes
the existing serial copy loop. It now records CopyWorkers=1, matching standard
Go's serial fallback. No allocator, rollback, source poisoning, card, pressure,
or native-view refresh algorithm is replaced. No raw pointers to moved payloads
are introduced. Backing ownership remains typed Go slice references; guest
references remain stable numeric handles.

Required young run admission still uses the same credit proof. Required LOS
failure latches pressure; optional refusal remains successful collection, and
successful relocation consumes pressure. Recoverable injected copy/commit
failure restores allocator metadata before returning without publishing new
handles. The wagodebug tests run the actual TinyGo compiler's panic/defer/recover
and failure hooks, including first/last partial copies and committed-maintenance
retry. Non-debug modes do not compile artificial failure hooks.

The foreign-stack prerequisite remains byte-identical: patched TinyGo 0.42.0
scans registered mappings plus normalized host spans. This slice neither changes
that registry nor suppresses the compiler runtime's GC. Joint target tests retain
forced concurrent foreign-stack GC, trap/interruption, parked host roots,
reentry, prepared entry and Close-failure cleanup. Those tests qualify the pinned
boundary fixtures, not H6's separate host ownership graph/wakeup gate.

## Source and target limits

Slipstream 82d546679bab4887acfab056f6ad773a969b1e03 gc/native.goose was read
before triage: ng_evacuation consumes explicit pressure and uses a participant
reservation count independently of active helpers. Its raw-address source and
threading ABI has no TinyGo build gate or CopyWorkers diagnostic. The Wago stub
omission is target-specific; no matching sibling defect is established.
Starshine e075c47e61b01ab9e4fd0ae4a7ca47b755e3fca1's GC boundary document covers
core types/instructions/validation, not this native collector/compiler path.
Sibling sources were read only; exact hashes are in sibling-audit.json.

ObjectEdgeSource and reclaim callbacks remain excluded on TinyGo by
object_edges_other.go and reclaim_other.go. This lane does not enable a core
allocation/callback API that is disabled on that target, nor claim managed host
edge/lifetime receipt support from serial heap movement. No H6-owned object edge,
object trace, reclaim, host graph, receipt, frontend or backend safepoint file is
modified. H2 remains gated by the separately owned H6 qualification.

Execution qualification is Linux amd64 with the pinned patched compiler only.
No unsupported architecture or scheduler execution is inferred from build tags.
No benchmark, timing experiment, measurement lease, push, PR, or PR closure is
part of this slice. Incidental testing-framework elapsed fields are raw test
output, not performance evidence.
