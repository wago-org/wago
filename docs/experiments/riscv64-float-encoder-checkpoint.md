# RV64 scalar floating-point encoder checkpoint

The host-independent encoder now emits fixed-width RV64G F/D arithmetic,
square root, sign injection, min/max, comparisons/classification, raw register
transfers, precision conversions, signed/unsigned word/long conversions and
floating-point control/status access. No optional Zfa, compressed or vector
instructions are used. This stage does not admit floating-point Wasm modules.

Rounding operands accept static RNE/RTZ/RDN/RUP/RMM or dynamic mode and reject
reserved encodings before emission. Wasm lowering must use explicit static modes.
FMV.X.W sign-extends its raw low word; consumers must zero-normalize shared
32-bit carriers. FMV.W.X boxes the single-precision value in the wider FP register;
double-precision moves retain all bits. Arithmetic and conversion semantics follow
the ratified [F specification](https://docs.riscv.org/reference/isa/v20260120/unpriv/f-st-ext.html)
and [D specification](https://docs.riscv.org/reference/isa/v20260120/unpriv/d-st-ext.html).

Checked emission observes GP results independently from FP results. Every FP
definition kills its full 64-bit register identity, including upper NaN-box bits;
f0 is writable. Invalid operands, sticky errors and unaligned buffers produce
neither bytes nor observer notifications. FCSR/fflags helpers expose only those
floating-point CSRs, rather than arbitrary system state.

All 54 typed emitters have independently assembled LLVM 19 golden words and a
live RV64G assembler comparison. Diagnostic tests decode GP writes independently
and check separate bank identities, f0/f31 boundaries, full-width kills and
validation atomicity. Host encoder/compiler and QEMU encoder tests passed three
runs in ordinary and checked modes. Host/RV64 vet and strict staticcheck passed.
The force-linked ordinary AMD64/RV64 diagnostic dead-code check includes FP
emission; integer compiler fingerprints remain identical across diagnostic modes.
Evidence is in the task workspace's `evidence/float-encoder-*.log`.

Independent source review accepted this encoder stage after transfer comments
and focused symbolic regressions were added. FCSR preservation, guest ABI/state
sentinels, genuine Wasm FP lowering and native hardware qualification remain
separate follow-up stages.
