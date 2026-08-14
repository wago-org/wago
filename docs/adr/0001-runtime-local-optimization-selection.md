# Keep optimization selections runtime-local

Optimization Selections are immutable members of runtime compilation
configuration rather than mutable process-wide CLI state. Railshot resolves a
complete selection into a bounded two-word policy before module work and carries
pre-resolved options through compiler-owned state. Compilation must not install
temporary values in architecture globals or retain a process-wide lease. Callers,
artifact identity, and concurrent Runtime Installations therefore observe only
their runtime-local selection while the direct compiler remains single-pass.
