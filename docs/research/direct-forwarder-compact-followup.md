# Direct forwarders and omitted compact bodies

Base: `48849d58b5b0a51e0646f02c2eaeccb58ea14129` on PR1008.
Review: https://github.com/wago-org/wago/pull/1008#discussion_r4333439004.

A valid module can call a wrapper whose sole call targets a tiny leaf. Compact
inline planning removes that leaf's standalone body after inlining it into the
wrapper. The later forwarder pass used to redirect the outer caller to the
removed body. The linker correctly rejected the resulting relocation.

The forwarder pass now retains the wrapper edge when the terminal target is
eligible for compact body omission. It uses the existing omission predicate,
including the `WAGO_INLINE_DEAD_BODY` rollback. The immutable inline target
table remains visible even to callers that reserve no inline locals; no new
local slots or module-sized maps are allocated. The guard is conservative:
it also retains the wrapper if a compact omission candidate has a host adapter
that ultimately keeps its body. Ordinary noncompact targets remain eligible
for retargeting, as do compact targets when body omission is disabled.

Re-inlining after retarget was deliberately avoided. A general tiny leaf can
need parameter/local slots that the original caller did not reserve. Keeping
its original wrapper call requires no new inline plan or source-mask handling.
The call still consumes its original source mask exactly once in `callInternal`.
`finalizeOmittedInlineEntriesAMD64` continues to reject all relocations to truly
omitted bodies; its safety check and the entry-alias rules are unchanged.

## Regression and checks

`TestDirectForwarderCompactOmittedLeaf` checks identity and add-constant leaves,
one and four wrapper hops, and serial and parallel compilation. It requires
actual body omission, checks diagnostic omission/inline/retarget counters when
available, and executes every compiled module through the native engine.
`TestDirectForwarderRetainsNormalInlineTargetLowering` requires the existing
retarget counter and native result in ordinary mode and with compact omission
disabled. These execution checks run in ordinary builds too.

The strict regression first failed all eight variants with `retains relocation
to omitted inline body` (3.683 s including Go compilation). A guard-only attempt
still failed because callers without original inline sites had no target table;
that attempt and its focused suite failures remain in the data. Moving the
table assignment exposed a test cleanup mistake: parallel compilation can
return code bytes without a `CodeImage`. The cleanup now closes a code image
only when present, while every variant still maps and executes its code.
The corrected strict regression passed in 3.684 s.

Focused normal and `wago_codegenstats,wago_regalloccheck` runs cover both the
AMD64 backend and public API, including existing forwarder root masks, fresh
and saved artifacts, trap/source ordering, compact inline proofs and the
linker's residual-relocation rejection. Both passed: normal 24.870 s; diagnostic
plus register-allocation checker 25.192 s, including Go compilation.

```sh
GOMAXPROCS=2 CGO_ENABLED=0 go test -p=2 ./src/core/compiler/backend/railshot/amd64 ./src/wago -run '^Test(DirectForwarder|CompactInline|Inline|FinalizeOmittedInline)' -count=1
GOMAXPROCS=2 CGO_ENABLED=0 go test -p=2 -tags=wago_codegenstats,wago_regalloccheck ./src/core/compiler/backend/railshot/amd64 ./src/wago -run '^Test(DirectForwarder|CompactInline|Inline|FinalizeOmittedInline)' -count=1
```

All attempts and wall times remain in `data/direct-forwarder-compact-followup/`.
This is a compile-time admission fix; it adds no native runtime checks and makes
no performance claim. Broad suites are reserved for the combined integration
checkout after these focused checks pass.
