# Function local limits

`RuntimeConfig.WithMaxFunctionLocals` is optional early admission control for the
combined number of parameters and declared locals in one Wasm function.
`NewRuntimeConfig` defaults to 65,535, which is the largest count represented by
the current compiler metadata. Callers can select a lower value from 1 through
65,535. Zero and larger values are configuration errors.

This setting is not the native safety boundary. AMD64 and ARM64 lowering
independently reject a function whose actual frame, including width-dependent
local slots, headers, inlining, and operand spills, exceeds the native stack
fence. Integer overflow checks and stack-fence checks remain fail-closed. For
example, 65,535 `v128` locals can pass the declaration-count policy but still
fail native compilation because the frame is too large.

The local-count setting remains part of the artifact-cache key because it is a
compile admission setting. A cached artifact admitted with a larger configured
count cannot bypass a smaller compiler configuration.

## WasmGC root admission

Collector-reference liveness covers the configured local population and removes
locals dead at every collection or native-call site. Final safepoint and callsite
root vectors are variable-sized. The old 1,024-live-root semantic limit is
removed.

Root metadata remains exact. Every offset must be aligned, ordered, inside the
validated native frame, and valid after artifact decoding. Corrupted lengths,
offsets, callsites, or frame sizes fail closed. The common path through 64 roots
retains one-word liveness masks; wider functions use the flat bitmap arena and
variable-length final offset vectors.

The compile-time liveness working arena is still capped at 64 MiB. This is a
temporary compile-resource implementation limit, not a Wasm root-count limit.
See `docs/runtime-resource-model.md`.
