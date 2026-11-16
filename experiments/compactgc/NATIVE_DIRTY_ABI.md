# Compact remembered certificate ABI3

ABI3 extends the separate compact view72→88 bytes: Dirty pointer at72,
DirtyCount at80, four padding bytes at84. Layout16, checked geometry and
allocation capability are otherwise unchanged. ABI2 native artifacts are not
admitted under this version. No allocation statistics pointer is added.

Dirty bytes are read-only to native code. For a valid old object, dirty1 means
its parent has already been registered in the remembered queue or overflow scan.
A native reference store may reuse that certificate or store into a young parent.
A clean old parent must take the complete helper path BEFORE payload mutation.
Setting the dirty byte alone is not a correct barrier and is forbidden.

Successful promote-all collection clears certificates after tracing/rewriting;
failed preflight leaves them intact. Start/dirty backing remains stable, and
Close clears the descriptor's pointer/count. Execution and collection are
serialized; neither a payload address nor a certificate survives a safepoint.

Go1.27.1 source3341d4fc qualification: full compactgc package passed0.015s;
compact_reads-tag native TestCompact passed0.003s. Tests include first helper
store, raw second payload store, failed minor preserving registration, successful
minor/full retaining children, backing/version rejection, and Close clearing.
Native emitter qualification and performance measurements are separate.
