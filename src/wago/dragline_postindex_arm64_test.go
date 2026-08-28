//go:build arm64

package wago

import (
	"encoding/binary"
	"errors"
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func TestDraglineARM64PostIndexPreservesTrapOrderAndFirstStore(t *testing.T) {
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0x00, 0x20, 0x01, 0x3a, 0x00, 0x00, // i32.store8 offset=0
			0x20, 0x00, 0x2f, 0x01, 0x01, 0x1a, // i32.load16_u offset=1; drop
			0x0b,
		}))),
	)
	compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if _, err := instance.Invoke("run", I32(65534), I32(0x7b)); err == nil {
		t.Fatal("second post-index-chain access did not trap")
	}
	if got := instance.Memory().Bytes()[65534]; got != 0x7b {
		t.Fatalf("first store before second-access trap = %#x, want 0x7b", got)
	}
}

func TestDraglineARM64LoadPairReportsOrderedTrapSite(t *testing.T) {
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0x00, 0x28, 0x02, 0x00, // i32.load offset=0, Wasm pc 2
			0x20, 0x00, 0x28, 0x02, 0x04, // i32.load offset=4, Wasm pc 7
			0x6a, 0x0b,
		}))),
	)
	compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	for _, tc := range []struct {
		base uint32
		pc   uint32
	}{{65534, 2}, {65530, 7}} {
		_, err := instance.Invoke("run", I32(int32(tc.base)))
		var trap *TrapError
		if !errors.As(err, &trap) || len(trap.Frames) == 0 || !trap.Frames[0].HasProgramCounter || trap.Frames[0].ProgramCounter != tc.pc {
			t.Fatalf("load pair base %d trap = %#v, %v; want Wasm pc %d", tc.base, trap, err, tc.pc)
		}
	}
}

func TestDraglineARM64FloatingPairExecutes(t *testing.T) {
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.F32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0x00, 0x2a, 0x02, 0x00, // f32.load offset=0
			0x20, 0x00, 0x2a, 0x02, 0x04, // f32.load offset=4
			0x92, 0x0b, // f32.add
		}))),
	)
	compiled, err := Compile(NewRuntimeConfig().WithCompiler(CompilerDragline).WithTarget(TargetNative), module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	memory := instance.Memory().Bytes()
	binary.LittleEndian.PutUint32(memory[128:], math.Float32bits(1.25))
	binary.LittleEndian.PutUint32(memory[132:], math.Float32bits(2.5))
	result, err := instance.Invoke("run", I32(128))
	if err != nil || len(result) != 1 || uint32(result[0]) != math.Float32bits(3.75) {
		t.Fatalf("floating pair result = %#v, %v", result, err)
	}
}
