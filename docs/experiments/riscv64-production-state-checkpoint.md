# RV64 production memory/global/bulk checkpoint

The production compiler envelope now admits one defined nonshared memory32,
integer globals with plain constant initializers, and active/passive data with
integer loads/stores, memory.size/grow, memory.copy/fill/init and data.drop.
Imports, starts, tables, shared/memory64/multiple memories and broader features
remain rejected before image creation. Public backend binding remains disabled;
the envelope supplies code, while production instantiation must own and install
the corresponding descriptors and payloads.

Production lowering reads the pinned JobMemory home, never the experimental
GuestContext. Memory bounds use the authoritative u64 byte cache, including the
full 65,536-page/4-GiB size. Effective addresses add canonical unsigned i32
carriers and immediates in 64 bits. Full range checks precede any byte access or
bulk mutation, including zero-count calls. Unaligned accesses use byte operations.
Growth checks a 64-bit page sum against the memory maximum and optional instance
quota, then updates current pages, authoritative u64 bytes and legacy u32 bytes.
Zero growth follows existing targets' size-query bypass. The stable reserved
mapping never moves; this stage does not use guarded memory.

Globals use the production directory of aliased cell pointers. I32 reads use LWU
and writes replace the complete eight-byte cell with a zero-normalized carrier;
i64 reads/writes preserve all bits. Passive data descriptors use a u64 pointer,
u32 length and u32 padding. data.drop writes only the length. Large descriptor
indexes recompute addresses after long branches so branch scratch cannot destroy
the live payload pointer. No legacy X24 context load is emitted for production.

Two independent reviewers accepted the state boundary. Suggested focused
regressions were added for successful-store adjacent bytes and dropped-segment
source=1/count=0. Other fixtures cover every integer load/store width, dirty i32
addresses, overlapping copy in both directions, quota/overflow failures,
callee growth observed by a caller, active/passive segment indexing, descriptor
padding and neighbors, zero-memory cases, trap reuse and output immutability.
A lazily backed 4-GiB reservation touches only small test ranges and its last
page; it verifies the last memory32 byte and rejection of a carried immediate.

Full compiler suites passed three ordinary and three checked-register QEMU runs.
Host compiler/encoder/ABI suites passed three runs in each mode, with focused
race checks and an existing-target scalar/control/call reference run. Host/RV64
vet and staticcheck with the existing unused-test-field exclusion passed.
Ordinary/checked fingerprints now include both experimental and production
emission, with matching code and absent ordinary diagnostic implementation
symbols. Documentation checks passed. Logs are in the task workspace's
`evidence/production-state-*.log` files.

QEMU used fixed-width RV64G with Zba/Zbb/Zbs/Zicond/V disabled. This is emulated
correctness evidence; native cache/performance qualification is unrun.
Real public instantiation/import/resource ownership and broader feature lowering
remain in [the parity tracker](riscv64-parity-tracker.md).
