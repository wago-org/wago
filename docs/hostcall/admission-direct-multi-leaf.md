**ADMIT — design admission only**, against Wago `90f2b57da`. Multiple distinct, independently machine-proven i32 leaves can safely use instance-owned import dispatch cells without decoder expansion or public API changes. **Require all-or-nothing admission for the new instance route.** Qualification remains outstanding.

I read the full brief, all three census reports, and pinned Slipstream safety contract v2. Inspection was read-only; no edits, builds, tests, timings, benchmarks, PRs, CI actions, children, or excluded-campaign access occurred.

**Smallest safe design**

Preserve the existing single-import/same-PC-alias path. Add a separate distinct-leaf path that:

1. Applies the existing instance-wide eligibility gates.
2. Independently acquires a retained, certified leaf for every resolved ordinary import.
3. Associates each import index with its own retained leaf.
4. Replaces only that import’s instance-owned dispatch cell.
5. Leaves the control-frame trampoline generic for the new path.
6. Sets `executionFlagNativeScalarLeaf` before the instance becomes executable.

The generic trampoline is essential: existing staged leaf wrappers compare the packed import-index/shape word against **index zero plus the leaf’s arity**. Selecting an arbitrary member as the frame-wide leaf would execute the wrong callback for some index-zero calls. Evidence: `native_leaf_amd64.go:254–329`, `native_leaf_arm64.go:124–208`, and `engine_host.go:260–274`.

Shared host thunks, reference descriptors, reexports and table/reference routes can retain their current generic entries. Their retained callable and owner metadata must remain intact.

**Exact admission gates**

Retain:

- `NativeScalarLeafSupported`, `!codeProfileEnabled`, and `in.c.nativeScalarLeafAllowed()`.
- Nonempty `syncHosts`, with `len(syncHosts) == in.c.NumImports`.
- No local GC or imported/dynamic/store-owned collector domain.
- Every binding ungated and exactly `syncHostTypedI32` or `syncHostTypedI32x2`.
- Existing Go ABI/version, exact callback type, function-entry, bounded instruction and wrapper-register proofs.
- A usable direct wrapper for **every** binding.
- A complete, valid instance-owned dispatch array; each selected cell must correspond to the resolved binding and expected signature.

Do not infer eligibility from names, arity, a native pointer, or the first successful callback. Captured-state bodies remain rejected by the existing decoder. Different unary/binary shapes are admissible only through their independently validated wrappers.

Evidence: `host_execution.go:187–230,1083–1117`; `native_leaf_native.go:45–80`; architecture-specific body validators.

**Identity and executable lifetime**

Separate two identities:

- **Binding identity:** owning control frame plus import index, exact resolved signature, and retained callback/owner.
- **Reusable code identity:** existing `(PC, reflect.Type, params)` key on amd64/arm64.

Equal PCs may share code only because the machine proof excludes closure/receiver/state access. Sharing executable code must never merge import identity, reference identity or owner authority.

Extend the frame registration to own an immutable indexed collection of retained leaves, with a separately identified optional legacy trampoline leaf. `nativeScalarLeafPtr(ctrl)` must return only that legacy leaf, or zero for the new distinct route.

On amd64/arm64, reuse the locked code cache and reference counting. Account consistently per binding acquisition; aliases may share one cache entry. Final release follows instance quiescence, evicts the last cache owner and unmaps executable memory. No leaf pointer may outlive its retained owner. Evidence: `native_leaf_native.go:23–101`; `hostcall_layout.go:76–84`.

**Partial admission and rollback**

Partial per-import lowering is technically conceivable with the generic trampoline and conservative instance-wide scheduling. It is **outside this admission**: the requested mixed-binding fallback and smallest proof favor all-or-nothing.

If any binding is unknown, effectful, gated, unsupported or lacks a direct wrapper, release provisional acquisitions and keep the entire instance generic.

Installation must be transactional:

- Acquire and validate all leaves before modifying cells.
- Save original dispatch code/context words.
- Publish only after all prerequisites succeed.
- On any later failure, restore modified cells and trampoline state, clear newly installed flags, then release retained code.
- Include failure of `railshotRegisterNativeHostOwner` in rollback.

Current registration merely ignores individual `BindNativeScalarLeafImport` return values; the new path needs explicit success checking. Never unmap code while a surviving cell points into it.

**Scheduling and architecture consequences**

| Architecture | Required treatment |
|---|---|
| amd64 | Reuse existing direct argument/result-home wrapper, RBX preservation and tagged context shortcut. No new register ABI. |
| arm64 | Preserve direct-wrapper selection and its scratch-register proof. A validated body can lack a usable direct wrapper; reject the new transaction in that case. Preserve the register entry at **direct wrapper + 192 bytes**, which existing compiler shortcuts depend on. |
| riscv64 | It already supports native leaves, but uses a separate **per-frame mapping registry**, not the shared amd64/arm64 cache. Extend ownership per binding without introducing cross-frame sharing: its staged wrapper embeds the control address. Preserve its existing dispatch-tag behavior and foreign-control guard. |
| Unsupported/TinyGo configurations | Keep generic stubs and existing support gates. |

Evidence: `native_leaf_amd64.go:254–329`; `native_leaf_arm64.go:15–17,151–208`; `native_leaf_riscv64.go:17–57,193–286`; compiler `amd64/call.go:1943` and `arm64/call.go:1963,2040–2078`.

Any successfully installed native leaf must disable `boundedHostSegments()`: removed callbacks are no longer Go safepoints. Retain syscall scheduling, guest entry/backedge polling and cancellation. **Do not broaden isolated native entry:** `boundedModuleHostCode(..., true)` currently requires exactly one import. Evidence: `host_segments.go:21–23,53–85`.

**Code-evident costs removed**

For newly admitted direct calls, the existing wrappers avoid ordinary Go callback execution, park/resume, generic dispatch and applicable control-frame staging. Existing tagged compiler routes can also avoid context publication; arm64 may use its existing register entry.

This removes neither per-call reflection nor a universal allocation: those costs are already absent from relevant existing lanes. amd64 still uses argument/result homes, and surrounding spill/global work remains where the existing emitter requires it. No speedup is claimed.

**Required later proof**

Require route-asserted forced-generic versus fast comparisons for distinct unary/binary leaves, aliases and import-order permutations; raw poisoned i32 carriers; preserved locals/globals/context; generic reference/table/reexport/tail and cross-instance routes; profiling/GC/plugin/unsupported-body rejection; cancellation; cache sharing and close; and injected failure after each acquisition/publication stage.

Run ordinary, `wagodebug`, `wago_regalloccheck`, applicable race/package gates, and separate backend execution qualification. Update the existing “different pure bodies must reject” expectation in `native_leaf_alias_test.go` while retaining captured-state rejection.

Read-only sibling checks confirmed the boundary: Slipstream authenticates resolved implementation/signature in `src/jit.goose:176–210`; Starshine’s byte adapters manipulate handles, allocation and cursors in `node/internal/runtime.js:171–198`. Neither supplies a transferable Wago ABI or reason to weaken its machine proof.

**Result: ADMIT with transactional, all-or-nothing per-instance installation; retain existing single/alias behavior and generic addressable fallback.**
