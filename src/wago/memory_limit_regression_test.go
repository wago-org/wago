//go:build (linux || darwin || windows) && (amd64 || arm64)

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func fullMemory32Module() []byte {
	limits := append([]byte{0x01}, wasmtest.ULEB(65536)...)
	limits = append(limits, wasmtest.ULEB(65536)...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(5, wasmtest.Vec(limits)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("size", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("last", byte(wasm.ExternFunc), 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x3f, 0x00, 0x0b}),
			wasmtest.Code([]byte{
				0x41, 0x7f, // i32.const -1
				0x41, 0xda, 0x00, // i32.const 90
				0x3a, 0x00, 0x00, // i32.store8
				0x41, 0x7f, // i32.const -1
				0x2d, 0x00, 0x00, // i32.load8_u
				0x0b,
			}),
		)),
	)
}

func growingToFullMemory32Module() []byte {
	limits := append([]byte{0x01}, wasmtest.ULEB(65535)...)
	limits = append(limits, wasmtest.ULEB(65536)...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(5, wasmtest.Vec(limits)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("grow", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("size", byte(wasm.ExternFunc), 1),
			wasmtest.ExportEntry("last", byte(wasm.ExternFunc), 2),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 0x01, 0x40, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x3f, 0x00, 0x0b}),
			wasmtest.Code([]byte{
				0x41, 0x7f,
				0x41, 0xda, 0x00,
				0x3a, 0x00, 0x00,
				0x41, 0x7f,
				0x2d, 0x00, 0x00,
				0x0b,
			}),
		)),
	)
}

func TestMemory32FullFourGiBBoundary(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), fullMemory32Module())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	if compiled.MemMinPages != 65536 || compiled.MemMaxPages != 65536 {
		t.Fatalf("compiled limits = %d..%d, want 65536..65536", compiled.MemMinPages, compiled.MemMaxPages)
	}
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if got, err := in.Invoke("size"); err != nil || len(got) != 1 || got[0] != 65536 {
		t.Fatalf("memory.size = %v, %v; want [65536]", got, err)
	}
	if got, err := in.Invoke("last"); err != nil || len(got) != 1 || got[0] != 90 {
		t.Fatalf("last-byte round trip = %v, %v; want [90]", got, err)
	}
}

func TestMemory32GrowReachesFullFourGiBBoundary(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), growingToFullMemory32Module())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if got, err := in.Invoke("grow"); err != nil || len(got) != 1 || got[0] != 65535 {
		t.Fatalf("memory.grow = %v, %v; want [65535]", got, err)
	}
	if got, err := in.Invoke("size"); err != nil || len(got) != 1 || got[0] != 65536 {
		t.Fatalf("memory.size after grow = %v, %v; want [65536]", got, err)
	}
	if got, err := in.Invoke("last"); err != nil || len(got) != 1 || got[0] != 90 {
		t.Fatalf("grown last-byte round trip = %v, %v; want [90]", got, err)
	}
}

func TestNewMemoryRejectsOversizedAndPreservesFourGiBLimit(t *testing.T) {
	if _, err := NewMemory(65537, 65537); err == nil {
		t.Fatal("NewMemory accepted more than 65536 memory32 pages")
	}
	memory, err := NewMemory(65536, 65536)
	if err != nil {
		t.Fatal(err)
	}
	defer memory.Close()
	if got := memory.jobMemory().CurrentPages(); got != 65536 {
		t.Fatalf("current pages = %d, want 65536", got)
	}
	if got := memory.jobMemory().MaxPages(); got != 65536 {
		t.Fatalf("maximum pages = %d, want 65536", got)
	}
	if got := len(memory.UnsafeBytes()); uint64(got) != uint64(65536)*65536 {
		t.Fatalf("byte length = %d, want 4294967296", got)
	}
}
