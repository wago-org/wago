# Independent unsupported-global blocker recheck

## Verdict

Reviewed immutable source `7e41fafbbcd737ed1edce11b94b43b4eda134dcf`
against review evidence `8526d061047dd4b6afd218157a5e4c709456a167`,
the original implementation `3242c18a19620aa77f39dd63cb5cd7544d4ca70a`,
and follow-ups `15d7b24b7` plus `7e41fafbb`.

**The unsupported-global blocker is closed for this adapter contract; no new
production blocker was found.** Both public ingress shapes now reject a known
opaque conversion word before the ordinary-directory fallback:

* `Global.SetValue` checks the physical global owner's conversion namespace.
* `Instance.SetGlobalValue` checks the accessing/publishing Instance and then
  the distinct physical owner namespace for an imported global.

The guard returns before storage mutation. Forged and cross-store tokens remain
unhandled by the conversion guard and reach the ordinary invalid-token
validation added by `c786a9b84`; ordinary and null values also retain the
ordinary path. Immutable and closed metadata/lifetime errors precede token
classification, including known opaque words. The focused tests assert STOP
before write for rejection cases.

This verdict is limited to the unsupported adapter implementation and tests.
It is not native Darwin execution credit, actual TinyGo execution credit, or
overall/full-port approval. The separate native Darwin ARM64 scope at
`3f616188315096d7ce1b5fe96b747516f99ffff0` remains governed by its native
harness owner and is not re-approved here.

## Static review

`setConversionGlobalPublic` reads global-owner metadata under `globalOwner.mu`
and each conversion entry set under `gcExternConversionState.mu`. It performs
no object dereference, root acquisition, root retirement, or publication.
Recognized opaque words return an error before either public setter's ordinary
write path. Unrecognized words are authenticated against the selected physical
store only to decide whether they are ordinary; invalid/cross-store words are
left to the existing setters so their established diagnostic and locking order
is preserved.

The early immutable/closed checks do not weaken validation: they only defer to
the public API where publication is impossible. `Global.SetValue` then acquires
the owner invocation/native-state lifetime; `Instance.SetGlobalValue` acquires
the accessing Instance invocation/native-state lifetime. No unsafe read or lost
root path was found in the repair.

Build selection remains unchanged. The modified fallback file is selected for
unsupported adapters and tagged TinyGo tests; RV64 exits through its existing
compact gate. Linux and Darwin host conversion implementations are untouched.
Thus the RV64 compact protocol, native host paths, and actual TinyGo support
claims are unchanged.

## Independent immutable checks

The exact source archive SHA-256 is
`9ca226384ac27874ae0ffd36030931f805b232e963dcb1475f5c87b84660a3f5`.
Reviewed file hashes and exact pins are in `source.json`. Go is
`/home/dev/.local/share/mise/installs/go/1.27.1/bin/go`, SHA-256
`30969f97169d7f43fe6a085873d75613adc21e30818a8c61d95bd27275df4624`.

All six current-source cells passed: the complete
`^TestUnsupportedConversion` selector under ordinary, `wagodebug`, and
`wago_regalloccheck`, both normally and with Go race. These tests cover direct
`Global.SetValue`, imported/access-instance namespace recognition, ordinary and
null writes, forged and cross-store invalid tokens, immutable-before-invalid,
immutable-before-null, closed-before-invalid, closed-before-null, and known
opaque immutable/closed precedence. The `wago_target_tinygo` runs are adapter
tests, not actual TinyGo execution.

The temporary negative-control probe has SHA-256
`58b86e8c857b45b7a8e210e66f241dc6de12d063db1ca0ff931269c05061ca1f`.
It fails as expected at immutable pre-fix source `3242c18a1`: the distinct
access namespace returns `handled=false, err=nil`. The probe existed only in
the temporary archive extraction and did not modify production source.

An initial control attempt used evidence pin `8526d0610`, but that evidence
commit already contains `15d7b24b7`; the probe correctly passed. That bounded
failed setup attempt is preserved in `failed-attempt-8526-negative-control/`
with its logs, results, and provenance rather than being discarded.

## Sibling audit and boundaries

Read-only audit of Slipstream
`dcb45b56386327a1f71fb039ed02e13562e1e2d7` confirms its distinct design:
imported globals are physical regions and externalized managed externrefs use a
compact tagged representation. It has no Wago-style per-Instance opaque public
conversion namespace, so no sibling defect is claimed. Starshine
`1d6c0af47140299d8f3c01ed03060d279c43ee03` contains frontend imported-global
and externref handling but no analogous native public conversion-store ingress;
again no sibling defect is claimed. Neither sibling was modified.

No workflow/source edit, benchmark, push, dispatch, PR, subagent, native Darwin
run, QEMU run, or actual TinyGo run was performed. Previous full host/QEMU
receipts at `3242c18a1` are background only and do not qualify these follow-ups.
