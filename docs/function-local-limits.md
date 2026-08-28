# Function local limits

`RuntimeConfig.WithMaxFunctionLocals` bounds the combined number of parameters
and declared locals in one Wasm function. `NewRuntimeConfig` defaults to 4,096;
callers may select any value from 1 through 65,535. Zero and values above the
unsigned 16-bit maximum are configuration errors.

This is an early validation and compiler-bookkeeping ceiling, not a native
stack-size promise. AMD64 and ARM64 lowering independently reject a function
whose actual frame, including width-dependent local slots, headers, inlining,
and operand spills, exceeds the 256 KiB stack-fence headroom. Raising the local
ceiling never weakens that fail-closed frame check. For example, a configured
65,535-local ceiling admits validation of the declaration but a function with
65,535 `v128` locals still fails native compilation because its frame is too
large.

The limit is part of the automatic artifact-cache key. A cached artifact built
under a larger ceiling therefore cannot bypass a smaller caller policy.

## WasmGC root admission

Collector-reference locals use the same declaration ceiling, but the separate
native root-map limit applies to values simultaneously live at a collecting
site. Liveness first covers the configured population, then removes locals dead
at every allocation or native call. Each emitted safepoint and callsite remains
limited to 1,024 exact roots. A function may consequently declare more than
1,024 reference locals when its live site maps stay within that bound; a site
with 1,025 live collector locals fails closed with an actionable admission
diagnostic.

The liveness bitmap is compile-only. Its main arena is capped at 64 MiB for an
adversarial combination of body size and configured locals. The CFG omits
instructions that cannot affect local liveness or control flow before sizing
that arena, and serialized metadata contains only final site offset vectors of
at most 1,024 entries. The common path through 64 roots retains one-word masks.

On the Ryzen 7 8845HS development host, the permanent 1,138-declared/one-live
analysis benchmark measured a warmed median of 17.2 us, 29,408 B/op, and 13
allocations. The existing dense 1,024-root benchmark measured 160.8-164.2 us,
224,082-224,084 B/op, and 10 allocations. These are compile-time costs; runtime
safepoint lookup and the final one-root metadata vector do not retain the
declaration-wide bitmap.
