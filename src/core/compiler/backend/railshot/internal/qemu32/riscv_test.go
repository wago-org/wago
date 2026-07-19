package qemu32_test

import (
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/internal/qemu32"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv32"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func TestRVClientExecutesPersistentFirmwareAndHelpers(t *testing.T) {
	qemu, err := exec.LookPath("qemu-riscv32")
	if err != nil {
		t.Skip("qemu-riscv32 not installed")
	}
	module, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.F32, wasm.F32}, []wasm.ValType{wasm.F32}),
		)),
		wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{1})),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 0, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("next", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("add", byte(wasm.ExternFunc), 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x23, 0, 0x41, 1, 0x6a, 0x24, 0, 0x23, 0, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x92, 0x0b}),
		)),
	))
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := riscv32.CompileModule(module)
	if err != nil {
		t.Fatal(err)
	}
	opts := riscv32.FirmwareOptions{BaseAddress: qemu32.ImageBase, NativeStackLimit: 1, HelperEntries: qemu32.RVHelpers()}
	size, err := riscv32.FirmwareImageSize(compiled, opts)
	if err != nil {
		t.Fatal(err)
	}
	image, err := riscv32.BuildFirmwareImage(make([]byte, size), compiled, opts)
	if err != nil {
		t.Fatal(err)
	}
	elf, _, err := qemu32.RVELF(image.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "rv-firmware.elf")
	if err := os.WriteFile(path, elf, 0o755); err != nil {
		t.Fatal(err)
	}
	client, err := qemu32.Start(qemu, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			t.Errorf("close qemu: %v", err)
		}
	}()
	next := image.TransportFunctions[0]
	for want := uint32(1); want <= 2; want++ {
		results, trap, err := client.Call(next.Address, next.Context, nil, 1)
		if err != nil || trap != 0 || len(results) != 1 || results[0] != want {
			t.Fatalf("next %d: results=%v trap=%d err=%v", want, results, trap, err)
		}
	}
	add := image.TransportFunctions[1]
	results, trap, err := client.Call(add.Address, add.Context, []uint32{math.Float32bits(1.5), math.Float32bits(2.25)}, 1)
	if err != nil || trap != 0 || len(results) != 1 || results[0] != math.Float32bits(3.75) {
		t.Fatalf("add: results=%x trap=%d err=%v", results, trap, err)
	}
}
