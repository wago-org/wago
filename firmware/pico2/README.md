# Pico 2 no-cgo firmware boundary

The Pico 2 integration is Go/TinyGo plus one small target assembly boundary. It
uses no C source, cgo, CMake, Pico SDK runtime, or mixed-language runtime shim.

The reusable pieces live in `src/core/runtime/embedded32`:

- `TransportEndpoint` implements the bounded framed protocol;
- `ServeTransportOnce` performs exact stream reads/writes with caller storage;
- `FirmwareTransportRunner` enforces instantiate/start/call/reset lifecycle;
- `FirmwareImageInvoker` transactionally restores and patches a linked image;
- f32, f64, i64, and SIMD helpers retain their fixed pointer-only 32-bit ABIs.

## Prepare the qualified TinyGo root

No prebuilt or custom firmware image is published. Build both targets from the
checked-in Wago firmware source and the exact TinyGo 0.41.1 source patch:

```sh
git clone --depth 1 --branch v0.41.1 \
  https://github.com/tinygo-org/tinygo /tmp/tinygo-wago-0.41.1
git -C /tmp/tinygo-wago-0.41.1 apply \
  "$PWD/firmware/pico2/tinygo/tinygo-v0.41.1-rp2350-riscv.patch"
export TINYGOROOT=/tmp/tinygo-wago-0.41.1
```

The patch includes the RP2350 USB status-clear fix, the Hazard3 startup and
interrupt path, RP2350 ROM/watchdog corrections, the RISC-V IMAGE_DEF and
linker script, the `pico2-riscv` target, and a non-consuming USB CDC receive
observer used for in-flight cancellation. It applies cleanly to the official
TinyGo `v0.41.1` tag. Use a TinyGo 0.41.1 executable with this root; mixing the
patch with another TinyGo release is unsupported.

## Arm build and physical execution

The checked-in Cortex-M33 firmware accepts a checksummed compiled artifact over
USB CDC, restores its pristine snapshot into executable SRAM, patches its
helper and stack entries, publishes the instructions, and exposes the standard
instantiate/start/call/reset lifecycle.

TinyGo 0.41.1 needs the RP2350 USB status-clear fix included in the checked-in
patch. The unpatched 0.41.1 image boots, but USB CDC does not enumerate.

TinyGo only loads target assembly through a target's `extra-files`. The build
script creates a temporary derived target that includes `native_arm.S` without
modifying the TinyGo installation:

```sh
scripts/pico2-build.sh arm build/wago-pico2-arm.uf2
cp build/wago-pico2-arm.uf2 /path/to/RP2350/
```

The UF2 copy reboots the board. After its USB serial device appears, probe the
real transport frame from the host:

```sh
go run ./cmd/pico2-probe -port /dev/ttyACM0
```

The firmware uses a 128 KiB upload snapshot, a separate 128 KiB live image, and
a checked 32 KiB generated-code stack. It accepts sequential chunks up to 252
bytes. Query the live base, compile a closed Wasm module for that exact address,
then upload, instantiate, start, and call an export:

```sh
go run ./cmd/pico2-probe -port /dev/ttyACM0
go run ./cmd/pico2-image \
  -target arm32 \
  -base 0xBASE_FROM_PROBE \
  -o /tmp/fib-rec-arm.artifact \
  bench/corpus/fib_rec.wasm
go run ./cmd/pico2-probe \
  -port /dev/ttyACM0 \
  -upload /tmp/fib-rec-arm.artifact \
  -call 0 -args 28 -results 2
```

`fib_rec.wasm` exports `fib(i32) -> i64`, so its result is two little-endian
32-bit slots. The expected final line is:

```text
Pico 2 call: export=0 args=[28] results=[317811 0] result_u64=317811
```

To prove reset restores the uploaded snapshot and invoke it again without
re-uploading:

```sh
go run ./cmd/pico2-probe \
  -port /dev/ttyACM0 -reset -call 0 -args 10 -results 2
```

## Hazard3 RISC-V build and physical execution

The RISC-V firmware uses the same bounded Go transport and arenas with a small
RV32 native-entry shim. Its publication hook issues `fence` and `fence.i`, and
generated helper/function addresses remain even-aligned instead of using Arm's
Thumb bit.

TinyGo 0.41.1 has no built-in Pico 2 Hazard3 target. The checked-in patch
installs the qualified target, which uses `riscv32-unknown-none`, RV32IMAC plus
Zicsr/Zifencei, RP2350 RISC-V UF2 family `0xe48bff5a`, a first-page RISC-V
IMAGE_DEF, and the Hazard3 interrupt CSRs. It also selects RISC-V boot-ROM
lookup entries for 1200-baud BOOTSEL.
After preparing `TINYGOROOT` as above:

```sh
sh scripts/pico2-build.sh riscv build/wago-pico2-riscv.uf2
cp build/wago-pico2-riscv.uf2 /path/to/RP2350/
```

Compile and upload for the exact live address advertised by the board:

```sh
go run ./cmd/pico2-probe -port /dev/ttyACM0
go run ./cmd/pico2-image \
  -target riscv32 \
  -base 0xBASE_FROM_PROBE \
  -o /tmp/fib-rec-riscv.artifact \
  bench/corpus/fib_rec.wasm
go run ./cmd/pico2-probe \
  -port /dev/ttyACM0 \
  -upload /tmp/fib-rec-riscv.artifact \
  -call 0 -args 28 -results 2
```

Physical qualification on 2026-07-20 returned `317811`; a transport reset
followed by `fib(10)` returned `55` without another upload.

The qualification firmware arms the RP2350 watchdog after startup and refreshes
it while the transport is idle. A generated-code hang therefore resets the
whole device instead of permanently wedging USB. TinyGo's RP2350 watchdog path
must use the RP2350 PSM reset mask (`0x01ffffff`), a tick factor of one, and a
12-cycle watchdog tick divider; the RP2040 mask/factor and an unprogrammed
divider do not produce a reliable Hazard3 reset.

## Stream the Release 2 corpus

The opt-in physical-board test converts an official Release 2 script on the
host, compiles each executable module for the live SRAM base advertised by the
board, uploads one artifact, runs every following action/assertion, and replaces
the artifact at the next module. Set `WAGO_PICO2_TRACE=1` for one result/trap
line per action:

```sh
WAGO_PICO2_SERIAL=/dev/ttyACM0 \
WAGO_PICO2_SPECTEST_DIR="$PWD/tests/spec-v2" \
WAGO_PICO2_SPECTEST_FILE=i32 \
WAGO_PICO2_TARGET=riscv32 \
go test ./src/core/compiler/backend/railshot \
  -run '^TestPico2Release2BoardExecution$' -count=1 -v -timeout=10m
```

Omit `WAGO_PICO2_TARGET` (or set it to `arm32`) for Cortex-M33. The physical
Hazard3 `i32.wast` run passed one module and all 374 assertions.

The complete 147-file Hazard3 sweep on 2026-07-20 attempted every script in a
fresh process and probed Hello after every result. The board stayed available
for all 147 files. Of those files, 116 passed outright; 1,244 modules and 44,969
actions/assertions executed physically in total. The remaining 31 files ended
in explicit bounded-target limitations: 18 exceeded image, memory/table growth,
function-directory, or frame capacity, and 13 require imports or preservation
of a replaced registered module. There were no remaining unexplained result
mismatches. Full logs are retained outside the repository under
`/tmp/pico2-riscv-sweep-aligned-20260720` on the qualification host.

That sweep also found that Hazard3 traps every misaligned `lh`/`lw`/`sh`/`sw`,
while WebAssembly permits scalar access at any byte address. RV32 scalar memory
lowering now keeps a naturally aligned fast path and uses a checked bytewise
little-endian fallback for unaligned accesses. `float_memory.wast` consequently
passes all 6 modules and 84 assertions on the physical core; `address.wast`
passes all 4 modules and 255 assertions.

For a module with linear memory, the host runner reserves the largest whole-page
capacity that keeps both the encoded artifact and live image within the board's
advertised arena. The one-resident-module path currently rejects scripts that
require a replaced registered module, imports, target reads of exported
globals, or target-side instantiation-failure setup. These are explicit harness
gaps, not skipped Wasm assertions. Closing them requires bounded memory
read/write transport and a policy for preserving or rebuilding registered
providers; closed scripts such as `i32.wast` exercise the real board
immediately.

An incomplete, out-of-order, oversized, checksum-invalid, malformed, wrong-
target, or wrong-address artifact is never made executable. Beginning another
upload invalidates the old runner before it mutates the snapshot. Instantiate
and reset copy the pristine artifact image into the separate live arena.

Cancellation remains responsive while generated code is running. The USB CDC
receive interrupt feeds a 24-byte, allocation-free frame observer before
placing the same bytes in the serial ring. A complete checksummed cancel request
sets the live cancellation cell immediately; the request remains queued for the
normal ordered transport response after the generated call returns with
`TrapCanceled`. Reproduce this hardware gate with:

```sh
WAGO_PICO2_SERIAL=/dev/ttyACM0 \
WAGO_PICO2_TARGET=riscv32 \
go test ./src/core/compiler/backend/railshot \
  -run '^TestPico2BoardCancellation$' -count=1 -v -timeout=1m
```

This gate passed on physical Hazard3 hardware on 2026-07-20. The cancel frame
was sent 100 ms after dispatch, the generated infinite loop returned
`TrapCanceled`, the queued cancel acknowledgement followed, and a subsequent
Hello probe succeeded.

Only these board-specific operations remain outside portable Go:

1. switch to the dedicated generated-code stack and enter an arbitrary target
   address;
2. expose the four TinyGo helper entry addresses; and
3. issue the target instruction-publication barriers.

Those operations use the small `native_arm.S` and `native_riscv.S` shims,
following wago's existing no-cgo native-entry pattern. Transport, lifecycle,
image validation/mutation, helpers, and board I/O remain Go.

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
