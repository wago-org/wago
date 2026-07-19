# Pico 2 pure-Go firmware boundary

The Pico 2 integration is intentionally Go/TinyGo-only. It uses no C source,
cgo, CMake, Pico SDK runtime, or mixed-language helper object.

The reusable pieces live in `src/core/runtime/embedded32`:

- `TransportEndpoint` implements the bounded framed protocol;
- `ServeTransportOnce` performs exact stream reads/writes with caller storage;
- `FirmwareTransportRunner` enforces instantiate/start/call/reset lifecycle;
- `FirmwareImageInvoker` transactionally restores and patches a linked image;
- f32, f64, i64, and SIMD helpers retain their fixed pointer-only 32-bit ABIs.

Only two board-specific operations remain outside portable Go:

1. reserve/map the generated image's fixed SRAM range; and
2. enter an arbitrary generated-code address and expose the four helper entry
   addresses.

Those operations use small target assembly shims, following wago's existing
no-cgo native-entry pattern. Transport, lifecycle, image mutation, helpers, and
board I/O remain Go.

## Generate a Go image

After building a closed or linked firmware image, emit a Go descriptor and SRAM
reservation fragment:

```go
source, err := shared.RenderPico2LinkedFirmwareGo(bundle, rootModule,
    shared.Pico2FirmwareGoOptions{
        Package: "firmware",
        Symbol: "ApplicationImage",
        Target: embedded32.TransportTargetArm32, // or RISCV32
        MaximumPayload: 4096,
    })
linker, err := shared.RenderPico2FirmwareMemoryScript(
    "ApplicationImage", bundle.BaseAddress, uint32(len(bundle.Bytes)))
```

The generated Go source contains:

- a constant string snapshot suitable for flash;
- exact export slot metadata;
- every linked module context; and
- an `embedded32.FirmwareImageDescriptor`.

The linker fragment reserves the complete fixed-address SRAM range as `NOLOAD`
and asserts its address and size. It contains no C-facing symbols or initialized
RAM payload.

## TinyGo startup

Board startup binds the reserved SRAM and target assembly entries without
allocating:

```go
image := unsafe.Slice(
    (*byte)(unsafe.Pointer(uintptr(ApplicationImage.ImageAddress))),
    len(ApplicationImage.InitialImage),
)
ApplicationImage.Image = image
ApplicationImage.HelperEntries = boardHelperEntries() // f64, SIMD, i64, f32

native := boardNativeEntry{} // target assembly Start/Call implementation
invoker := embedded32.FirmwareImageInvoker{
    Descriptor: &ApplicationImage,
    Native: native,
    Publish: boardPublishInstructions,
}
runner := embedded32.FirmwareTransportRunner{
    Target: ApplicationImage.Target,
    MaximumPayload: ApplicationImage.MaximumPayload,
    ContextAddress: ApplicationImage.ContextAddress,
    StartAddress: ApplicationImage.StartAddress,
    Functions: ApplicationImage.Functions,
    Invoker: &invoker,
}
endpoint := embedded32.TransportEndpoint{
    ParameterSlots: parameterSlots[:],
    ResultSlots: resultSlots[:],
    PayloadScratch: payload[:],
    MaximumPayload: ApplicationImage.MaximumPayload,
}

for {
    if err := embedded32.ServeTransportOnce(
        &endpoint, &runner, boardStream,
        request[:], response[:],
    ); err != nil {
        boardProtocolError(err)
    }
}
```

`boardStream` is an allocation-free `io.ReadWriter` implemented with TinyGo's
board support or direct RP2350 MMIO. Cortex-M33 and Hazard3 builds use separate
assembly shims for generated entry and instruction publication; neither requires
a C ABI or C runtime.

`FirmwareImageInvoker` preflights all contexts, helper tables, callable entries,
and complete image bounds before mutation. Instantiate/reset copies the complete
snapshot, patches every linked module's helper table, and only then publishes
instructions through a non-failing target publication hook. Start and call clear
the selected context's trap cell before entering generated code.

## Validation

```sh
go test ./src/core/runtime/embedded32 -count=1
go test ./src/core/compiler/backend/railshot/shared -count=1
```

The tests cover allocation-free stream framing, transactional image restore,
linked helper-table patching, instruction publication, trap/cancellation cells,
callable-address validation, and generated Go source parsing.
