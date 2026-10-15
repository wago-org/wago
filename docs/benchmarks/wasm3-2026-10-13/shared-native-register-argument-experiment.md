> Historical experiment note. The qualified retained combination and current limitations are in [native-shared-parity.md](native-shared-parity.md).

# Direct arguments inside the guarded shared native body

Private follow-up to held `7fa59c74`, which regressed ordinary SDK threads by12–20%. This experiment removes the argument SP store/load only for one i32 argument in the already entry-guarded native-only body. Preceding roots must be nontrapping integer local/constant/canonical-slot values with canonical argument homes; regional residency and active/caller-clobbered pins retain the complete flush path. Popping/materializing the argument before canonicalizing these proven-safe preceding values preserves their evaluation/trap order. The native callback CALL and real guest memory accesses remain. Go/foreign paths are unchanged.

`WAGO_ARM64_SHARED_IMPORT_ARG_REG=0` retains parent argument staging. The result-register rollback also retains a correctly sized result spill slot. Focused source/artifact/native/Go/foreign/negative-result/cancellation/GC checker tests pass. Quick paired native timings are inconsistent and generally adverse for ordinary callbacks; independent SDK evidence is required. Broad validation and full qualification remain required before any retention. Source006 remains the PR/local publication.
