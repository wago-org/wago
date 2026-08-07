# Keep optimization selections runtime-local

Optimization Selections are immutable members of runtime compilation configuration rather than mutable process-wide CLI state. Railshot may temporarily adapt a selection to its existing architecture globals under a compile-scoped lock, but callers, artifact identity, and concurrent Runtime Installations must observe only the runtime-local selection; this preserves the current direct compiler while leaving a clear path to move each Optimization Binding into compiler-owned state.
