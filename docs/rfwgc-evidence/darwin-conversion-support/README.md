# Darwin ARM64 converted publication support

Implementation scope: host opaque conversion receipts plus the conditional
function-owner graph on Darwin ARM64, using the existing Linux algorithms.
Native Darwin execution is REQUIRED and remains pending parent dispatch.
Cross compilation proves build availability only. Core 3 totals 215/690 and the
child 10/18 expectation are unchanged; no CI, backend, reference_store.go or
gc_invocation_retirement.go changes belong to this slice.

## Architecture and platform audit

* `gc_conversion_receipt_host.go`: physical container slots own checked collector
  roots and domain claims before visible publication; distinct aliases have
  distinct receipts. Identities retain a domain, not a publishing Instance.
  Replacement, rollback, clear and owner/store Close retire the appropriate
  receipt; collector access requires registered domain authority.
* `gc_conversion_store_host.go` covers initializer cells/staged element ranges,
  public globals/tables, guest stores and bulk ranges. `gc_conversion_host_container_host.go`
  provides physical host-container lifetime and authentication. The complementary
  other files remain selected outside Linux host and Darwin ARM64, including
  TinyGo; RV64 retains its compact conversion protocol.
* `function_environment_*_native.go` (formerly `_linux.go`) implements typed
  aggregate owner edges, construction journals, joint marking, conditional
  persistent slots, deferred reclaim and quiescent retry. Host adapters preserve
  invocation-conservative environment roots and exact imported-owner attachment
  accounting. The bodies are unchanged. There is no conservative permanent
  retention substituted for eventual cycle release.
* `gc/native/{object_edges,object_trace,reclaim}_native.go` are Go sidecars using
  typed pointers, sync mutexes, atomic counters, immutable observer snapshots,
  and bounded descriptor/handle tracing. They contain no Linux syscalls, mmap,
  signal handlers or raw ABI offsets. Underlying collector heap allocation uses
  existing Go-backed spaces/handles; no new mapping or permissions contract is
  introduced. Callback execution requires the collector mutator lock and cannot
  reenter the collector. `parallel.go:drainParallel` declines parallel tracing
  when an object-edge source is installed; reclaim delivery follows collector
  free/Close, and observers are removed at Close.
* Darwin ARM64 already selects `gc_frame_roots_arm64.go`,
  `gc_frame_roots_runtime_arm64.go` and `gc_host_activation_arm64.go` with the same
  exact-frame/parked-activation contract as Linux ARM64. Host access uses
  `instance_native_context.go`'s native mutex selection. Graph work acquires the
  existing topology, ordered invocation/domain and native locks; sidecars add no
  signal-context callback and do not change the signal handler ABI. Static
  compatibility does not substitute for native execution qualification.
* No lock/retirement redesign: delayed cleanup under continuous readers and
  internal-callback panic behavior remain existing residual contracts.

## Read-only sibling mapping

Inspected Slipstream pinned imported-root fix `084be3d4bf70fb5337085ceddfab1bfa9db8b1c9`
`src/jit.goose:260-304`, and current locally fetched master
`d6802f5e389bded786f49b4c2a21df0efab7017c` at the same import-registration code.
Physical descriptor-keyed table/global root installation before exposure maps to
Wago's prepublication receipts. Inspected `ef06c3710a47b99e69ba981b5b9370aa4167e3a0`
and current `src/jit_store.goose`: failed ABI2 runtimes with potential nested
escapes remain Store-owned. That is NOT a counterpart for Wago's exact conditional
cycle/eventual-release graph. ABI2 raw offsets/mapping layout are not transplanted
into Wago's handles, opaque words, Go synchronization or checked root slots.
Starshine `1d6c0af47140299d8f3c01ed03060d279c43ee03`,
`docs/wiki/wasm-gc-core-boundary.md`, documents validation/codec/WAST boundaries;
no corresponding native physical-container ownership implementation was found
in this scoped audit. No sibling code was modified and no sibling defect is claimed.

## make(42) and positive initializer oracles

`TestConvertedMake42ClassificationAndReceipt` starts with the same null-initialized
variant used by the failed Darwin negative fixture. It separately asserts an
externref public result, a data conversion entry with a live managed object root,
distinct public/internal words, absence from the ordinary externref directory,
and recognition by `internalExternForGlobal`. This explains the old fallback:
`setConversionGlobalPublic` returned unhandled after the directory miss, then
`Global.setValueNoLease` recognized the opaque result and published it.
The test requires a physical receipt and domain claim before collection/access,
checks 42 after collection, checks receipt release on clear and domain retirement
on final Close. Native Darwin classification remains unverified until dispatch.

`TestConvertedInitializer55PersistentRoot` checks a real initializer receipt,
drives the production receipt owner-graph collection boundary, and then reads 55.
Staged initializer-only execution has no registered public frame state, so calling
public `CollectGC` on it is not the appropriate oracle. The failed development
attempts established that precondition and did not alter production behavior.
`TestExternrefGlobalHoldingConvertedGCReference` restores its original positive
7/42/null assertions on Darwin and adds postcollection 42.

The original failed Darwin negative fixture and logs remain at `58fbb7d4f` and
`docs/rfwgc-evidence/native-run-41431926958/`; none are overwritten. Expanded
existing H2 receipt/store/host-container/import and H6 nested-owner/wakeup tests
keep their original assertions, including bounded STOP checks before unsafe access.

## Validation

`check.py` records commands and exits for ordinary, wagodebug and
wago_regalloccheck. Development logs are not immutable qualification. Full/race,
collector/backend, cross-target and emulated execution receipts will identify
an immutable source pin separately. No timings, benchmarks, push, dispatch, PR
or child agents were used. Performance qualification requires an independent
measurement lease; native Darwin and final conformance gates remain open.

## Unsupported target guard

The fallback `gc_conversion_store_other.go` now checks the physical owner's
conversion namespace before deciding an ordinary-directory miss is unhandled.
This rejects known opaque public converted words before the legacy setter can
publish without a receipt. Null, ordinary tokens and invalid-token diagnostics
retain their previous paths. RV64 returns through its existing compact gate.
`TestUnsupportedConversionPublicGlobalRejectsOpaqueBeforeDirectory` is a
metadata-only negative test selected only on unsupported non-RV64 adapters.
Local `wago_target_tinygo` ordinary/debug/regalloccheck executions validate this
adapter logic; they are not native TinyGo execution qualification.

The first immutable full-suite attempt exposed missing gitlink fixture exports
and mise tool shims requiring trust in the temporary source directory. Logs in
`qualified-3f6161883` are retained as a failed setup attempt. The corrected runner
uses explicit installed tool paths and pinned submodule fixture archives.
