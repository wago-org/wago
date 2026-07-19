# Pico 2 firmware transport

This directory contains the allocation-free C boundary used to host a bounded
Arm32 or RV32 firmware image on RP2350.

`wago_pico2.c` mirrors `src/core/runtime/embedded32` exactly:

- 24-byte versioned little-endian frames and IEEE CRC32;
- hello/instantiate/start/call/cancel/reset lifecycle enforcement;
- exact serialized parameter/result-slot shapes;
- response-capacity preflight before a stateful call;
- per-export context selection for imported-function re-exports; and
- direct invocation of generated `CallABI` and start thunks on 32-bit targets.

The endpoint owns no storage. The application supplies request/response byte
buffers, parameter/result slot arrays, the mutable SRAM image, and a pristine
snapshot (normally in flash). `wago_pico2_direct_invoker` restores the complete
image before instantiation/reset, publishes the instruction range, invokes the
32-bit entry pointers, and writes cancellation through `ContextABI`.

## Pico SDK integration

Add the two runtime sources to a Pico SDK target and define
`WAGO_PICO2_PICO_SDK`:

```cmake
target_sources(my_firmware PRIVATE
    ${WAGO_ROOT}/firmware/pico2/wago_pico2.c
    ${WAGO_ROOT}/firmware/pico2/wago_pico2_pico_sdk.c)
target_include_directories(my_firmware PRIVATE
    ${WAGO_ROOT}/firmware/pico2)
target_compile_definitions(my_firmware PRIVATE WAGO_PICO2_PICO_SDK=1)
target_link_libraries(my_firmware PRIVATE pico_stdlib)
```

Initialize `wago_pico2_runner` from the generated image metadata, initialize a
`wago_pico2_endpoint` with fixed caller-owned buffers, then call
`wago_pico2_pico_sdk_serve`. The Pico SDK adapter uses the configured stdio
transport, so the same loop works with USB CDC or UART according to the board's
`pico_enable_stdio_*` settings.

The firmware image's helper table must point at four board functions implementing
the fixed f32, f64, i64, and SIMD helper-frame ABIs. Those helpers are part of
the generated image integration, not the wire endpoint; they must not allocate
or retain frame pointers.

## Validation

The Go runtime test compiles the portable endpoint with:

```sh
cc -std=c11 -Wall -Wextra -Werror -pedantic \
  firmware/pico2/wago_pico2.c firmware/pico2/wago_pico2_test.c
```

The self-test checks lifecycle ordering, CRC rejection, complete response
preflight, forwarded contexts, result publication, trap payload suppression,
and reset. Cross-compiling `wago_pico2.c` with the Pico SDK validates the same
source for both Cortex-M33 and Hazard3 builds.
