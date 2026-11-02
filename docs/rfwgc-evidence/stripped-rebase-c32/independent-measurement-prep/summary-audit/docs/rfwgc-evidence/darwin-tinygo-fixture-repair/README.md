# Darwin TinyGo fixture repair

Repairs the three fixture failures audited in native run 41453552853. The old run and all receipts remain FAIL and unchanged. Production implementation, compiler patch, deadlines, workflow bounds and source pin 7e41 remain unchanged.

The canonical runtime test now first checks actual nativeResumeStackTop against the preserved physical stack top. It then temporarily substitutes a guaranteed-distinct entry sentinel, checks the same physical top again, and restores the entry before Close. This rejects the wrong-field implementation deterministically without assuming unrelated virtual-memory boundaries differ. The pinned native export applies exactly the same test-only correction, recording before/after hashes.

The ARM64 stage8 test adapter removes precisely two dead-gc-new=false options, an AMD64-only optimization absent from ARM64. Wasm bytes, movement/generation checks, result42, trap and configuration controls remain unchanged. The source's AMD64 fixture is unchanged.

Cancellation now has required cancel and close subtests. InvokeContext cancellation checks errors.Is(context.Canceled), its documented contract; Close still requires the interrupted trap. Both paths retain eight GCs, the entry handshake, original five-second bounds, post-unwind GC, checked Close and final GC. Deferred cancel/Close ensure assertion failures also clean up. The import handshake proves import entry, not pure-loop instruction residency; no stronger claim is made. Missing either child fails census in all modes.

Cross-build results are compile-only. Native rerun and independent review remain required; no native PASS claimed here. This changes only tests/adapters and cannot affect shipped runtime performance. Historical suite-plan/manifests remain pinned historical receipts; this directory records the updated plan and validation.

Final local validation: all nine Darwin ARM64 Mach-O cross-builds PASS with unchanged export. Actual patched Linux TinyGo0.42 canonical resume positive PASS all3, wrong-field control fails all3. All exported Linux files match4913 Git blobs after restoring the deliberate one-line negative mutation. Initial Linux export omitted internal/runtimebridge and failed compilation; failed logs retained in linux-attempt-1. No native Darwin execution credited. All owned build/test commands finished.
