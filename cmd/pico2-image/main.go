// Command pico2-image compiles one closed Wasm module into a bounded 32-bit
// firmware image for an upload arena advertised by the Pico 2 transport.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/arm32"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv32"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/embedded32"
)

func main() {
	base := flag.Uint64("base", 0, "target upload base address")
	memoryCapacity := flag.Uint64("memory-capacity", 0, "linear-memory capacity in bytes (default: module minimum)")
	tableCapacity := flag.Uint64("table-capacity", 0, "table capacity in entries (default: module minimum)")
	stackLimit := flag.Uint64("stack-limit", 1, "native stack lower limit; use 1 before native-entry qualification")
	targetName := flag.String("target", "arm32", "generated-code target: arm32 or riscv32")
	output := flag.String("o", "", "output image path")
	flag.Parse()
	if flag.NArg() != 1 || *output == "" || *base == 0 || *base > uint64(^uint32(0)) ||
		*memoryCapacity > uint64(^uint32(0)) || *tableCapacity > uint64(^uint32(0)) ||
		*stackLimit == 0 || *stackLimit > uint64(^uint32(0)) {
		fatalf("usage: pico2-image -target arm32|riscv32 -base 0xADDRESS -o image.bin module.wasm")
	}
	target, normalizedTargetName := parseTarget(*targetName)
	wasmBytes, err := os.ReadFile(flag.Arg(0))
	if err != nil {
		fatalf("read module: %v", err)
	}
	module, err := wasm.DecodeModule(wasmBytes)
	if err != nil {
		fatalf("decode module: %v", err)
	}
	if err := wasm.ValidateModule(module); err != nil {
		fatalf("validate module: %v", err)
	}
	opts := shared.EmbeddedFirmwareOptions{
		BaseAddress:      uint32(*base),
		MemoryCapacity:   uint32(*memoryCapacity),
		TableCapacity:    uint32(*tableCapacity),
		NativeStackLimit: uint32(*stackLimit),
		// FirmwareImageInvoker replaces these placeholders with real board
		// helper entries before native execution is enabled.
		HelperEntries: [4]uint32{1, 1, 1, 1},
	}
	image, err := buildFirmwareImage(module, target, opts)
	if err != nil {
		fatalf("build firmware image: %v", err)
	}
	artifact := embedded32.FirmwareArtifact{
		Target:         target,
		ImageAddress:   image.BaseAddress,
		ContextAddress: image.ContextAddress,
		StartAddress:   image.StartAddress,
		Image:          image.Bytes,
		Contexts:       []uint32{image.ContextAddress},
		Functions:      image.TransportFunctions,
	}
	artifactSize, ok := embedded32.FirmwareArtifactSize(uint32(len(artifact.Image)), uint32(len(artifact.Contexts)), uint32(len(artifact.Functions)))
	if !ok {
		fatalf("firmware artifact size overflow")
	}
	encoded := make([]byte, artifactSize)
	if _, err := embedded32.EncodeFirmwareArtifact(encoded, artifact); err != nil {
		fatalf("encode firmware artifact: %v", err)
	}
	if err := os.WriteFile(*output, encoded, 0o644); err != nil {
		fatalf("write image: %v", err)
	}
	fmt.Printf("Pico 2 %s artifact: bytes=%d image_bytes=%d base=%#08x context=%#08x start=%#08x exports=%d output=%s\n",
		normalizedTargetName, len(encoded), len(image.Bytes), image.BaseAddress, image.ContextAddress, image.StartAddress, len(image.TransportFunctions), *output)
}

func parseTarget(value string) (uint32, string) {
	switch value {
	case "arm", "arm32":
		return embedded32.TransportTargetArm32, "arm32"
	case "riscv", "riscv32", "rv32":
		return embedded32.TransportTargetRISCV32, "riscv32"
	default:
		fatalf("target %q is not arm32 or riscv32", value)
		return 0, ""
	}
}

func buildFirmwareImage(module *wasm.Module, target uint32, opts shared.EmbeddedFirmwareOptions) (*shared.EmbeddedFirmwareImage, error) {
	switch target {
	case embedded32.TransportTargetArm32:
		compiled, err := arm32.CompileModule(module)
		if err != nil {
			return nil, err
		}
		size, err := arm32.FirmwareImageSize(compiled, opts)
		if err != nil {
			return nil, err
		}
		return arm32.BuildFirmwareImage(make([]byte, size), compiled, opts)
	case embedded32.TransportTargetRISCV32:
		compiled, err := riscv32.CompileModule(module)
		if err != nil {
			return nil, err
		}
		size, err := riscv32.FirmwareImageSize(compiled, opts)
		if err != nil {
			return nil, err
		}
		return riscv32.BuildFirmwareImage(make([]byte, size), compiled, opts)
	default:
		return nil, fmt.Errorf("unsupported target %d", target)
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "pico2-image: "+format+"\n", args...)
	os.Exit(1)
}
