# TinyGo mapped operands: unqualified experiment

Base: `e8b6c435`. Linux/amd64 only, explicit build tag
`wago_tinygo_mapped_operands`. Default TinyGo behavior is unchanged.

This enables the existing fixed-capacity Go mapping path, not a new allocator.
The mapping never grows after node pointers are published. Exhaustion uses heap
chunks. `elem` holds scalar storage and node links only; managed plugin values
and register slices are rooted separately in `stack.cold`. Every mapped/heap
chunk is independently retained in `stack.chunks`, so an off-heap node pointing
to a heap node is not the sole owner. Neither mapping contents nor addresses
stored in scalar fields are assumed to keep a Go object alive.

Serial compilation already defers closure. Parallel workers now independently
defer closure using the stable stack owner captured before lazy allocation;
they do not capture its initially nil arena or consult the cleared scratch
pointer. Other teardown panics cannot bypass this defer. Normal stack cleanup
still closes first; Close is idempotent and retries retained mappings after an
unmap failure. The existing finalizer is retained unchanged. No toolchain edits.

Source tests cover forced GC across mapped-to-heap links and managed cold roots,
fixed-arena exhaustion, closed-owner heap fallback, and independent deferred
cleanup across normal/error/panicking teardown. Existing runtime CodeBuffer
fault-injection tests cover failed allocation/growth/unmap and retry ownership.
Focused actual TinyGo GC/root/cleanup tests and full Go AMD64/runtime gates
passed; see [qualification](mapped-qualification/README.md). Full corpus, native
image identity, performance and RSS remain unqualified.
