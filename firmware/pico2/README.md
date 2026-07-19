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

Generate the image source and linker fragment after building a closed or linked
firmware image:

```go
source, err := shared.RenderPico2LinkedFirmwareC(bundle, rootModule,
    shared.Pico2FirmwareCOptions{
        Symbol: "application_wasm",
        Target: embedded32.TransportTargetArm32, // or RISCV32
        MaximumPayload: 4096,
    })
linker, err := shared.RenderPico2FirmwareLinkerScript(
    "application_wasm", "", bundle.BaseAddress, uint32(len(bundle.Bytes)))
```

The generated C file contains the pristine flash snapshot, a mutable SRAM array,
all transport export metadata, every linked module context, and a
`wago_pico2_image` descriptor. The generated GNU ld fragment places the mutable
array at the exact address used to compile the image, marks it `NOLOAD`, and
asserts both its address and size. Attach both files to the board target:

```cmake
wago_pico2_add_generated_image(my_firmware
    ${CMAKE_CURRENT_BINARY_DIR}/application_wasm.c
    ${CMAKE_CURRENT_BINARY_DIR}/application_wasm.ld)

# Cortex-M33 build:
wago_pico2_add_tinygo_helpers(my_firmware pico2)
# Hazard3 build (use instead of the line above):
# wago_pico2_add_tinygo_helpers(my_firmware riscv-qemu)
```

The TinyGo helper target emits a relocatable object from
`src/core/runtime/embedded32`, then localizes every symbol except the four fixed
helper entries. The Pico SDK retains its own reset, IRQ, allocator, and runtime
boundary, while final `--gc-sections` removes unreachable TinyGo startup code.
The `riscv-qemu` TinyGo target is used only as a freestanding RV32 helper-code
profile; no QEMU startup entry is retained in the board image.

Bind the four fixed pointer-only helper ABIs, initialize the generated image,
and serve with caller-owned storage:

```c
extern const struct wago_pico2_image application_wasm;

static struct wago_pico2_runner runner;
static uint32_t parameters[32];
static uint32_t results[32];
static uint8_t payload[4096];
static uint8_t request[WAGO_PICO2_TRANSPORT_HEADER_BYTES + 4096];
static uint8_t response[WAGO_PICO2_TRANSPORT_HEADER_BYTES + 4096];

int main(void) {
    const struct wago_pico2_helper_callbacks callbacks =
        WAGO_PICO2_EMBEDDED32_HELPER_CALLBACKS;
    struct wago_pico2_helper_entries entries;
    struct wago_pico2_endpoint endpoint;

    stdio_init_all();
    if (wago_pico2_helper_entries_init(&entries, application_wasm.target,
                                       &callbacks) != WAGO_PICO2_OK ||
        wago_pico2_runner_init(&runner, &application_wasm,
                               &entries) != WAGO_PICO2_OK) {
        return 1;
    }
    endpoint = (struct wago_pico2_endpoint){
        &runner, parameters, 32, results, 32,
        payload, sizeof(payload), sizeof(payload),
    };
    return wago_pico2_pico_sdk_serve(&endpoint, request, sizeof(request),
                                     response, sizeof(response));
}
```

`WAGO_PICO2_EMBEDDED32_HELPER_CALLBACKS` binds the exact exported symbols
`wago_embedded32_f64`, `wago_embedded32_simd_abi`, `wago_embedded32_i64`, and
`wago_embedded32_f32`. Their frame layouts are compile-time checked by the C
self-test. They allocate no transport storage and never retain a frame pointer.
`wago_pico2_runner_init` records their callable target addresses; every
instantiate/reset restores the full image and then patches every linked
module's helper table before publishing instructions.

The Pico SDK adapter uses the configured stdio transport, so the same loop works
with USB CDC or UART according to the board's `pico_enable_stdio_*` settings.

## Validation

The Go runtime test compiles the portable endpoint with:

```sh
cc -std=c11 -Wall -Wextra -Werror -pedantic \
  firmware/pico2/wago_pico2.c firmware/pico2/wago_pico2_test.c
```

The self-test checks lifecycle ordering, CRC rejection, complete response
preflight, forwarded contexts, result publication, trap payload suppression,
transactional helper-table patching, and reset. Generated image C is also
compiled with the same strict flags. Cross-compile both helper profiles with:

```sh
PATH="$(dirname "$(command -v tinygo)"):$PATH" \
WAGO_PICO2_TINYGO_HELPERS=1 \
  go test ./src/core/runtime/embedded32 \
    -run '^TestPico2TinyGoHelpers$' -count=1 -v
```

With TinyGo 0.41.1, the localized pre-link helper objects measure 20,111 bytes
text, 121 bytes data, and 1,704 bytes BSS for `pico2`; the RV32 object measures
21,417 bytes text, 32 bytes data, and 4,240 bytes BSS. Whole-firmware admission
uses the final Pico SDK link map after unreachable localized runtime sections
are removed, not these upper-bound object totals.
