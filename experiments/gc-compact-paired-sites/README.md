# Exact paired-loop selection diagnostic

Source `7483b3e0`, Go1.27.1, compile-only: `Runtime.Compile` with CoreFeaturesV3 and `dead-gc-new=false`. No guest execution or performance timing. Same source compiled with `wago_gc_compact_loop wago_gc_compact_refstruct` versus `wago_gc_compact_pair_loop wago_gc_compact_refstruct`. A third compile temporarily logged planner selections; its complete native bytes matched the uninstrumented paired build exactly. The instrumentation was removed afterward.

Both exact artifacts contain the checksum function, bulk kernel, scalar kernel, and exported performance wrapper. The wrapper selects which kernel is measured.

The only additional paired selection is function2, loop PC87, first access PC97, receiver locals1 and2, eight available GP registers. Checksum and initialization loop selections are unchanged.

Native entries change from `[0 736 4080 7360]` to `[0 736 4080 7424]`, total7535 to7599 bytes. Bytes `[0:4080]`, containing the checksum and complete bulk kernel, are identical. The scalar kernel grows64 bytes. The wrapper shifts64 bytes and differs only in its relative call relocation. Thus the observed bulk benchmark regression does not come from different bulk/checksum instructions; its cause remains unproven. This diagnostic does not claim a performance improvement.

Logs preserve exact Wasm/native SHA256 values. Segment comparison and wrapper disassembly preserve the byte-delta finding. The absolute paths in objdump headers identify temporary diagnostic files, not repository dependencies.
