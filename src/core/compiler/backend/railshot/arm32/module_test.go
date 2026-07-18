package arm32

import (
	"os/exec"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	a32 "github.com/wago-org/wago/src/core/encoder/arm32"
	"github.com/wago-org/wago/src/core/runtime/embedded32"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func arm32Module(t *testing.T) *wasm.Module {
	t.Helper()
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{1})),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 0x2a, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x41, 0x01, 0x6a, 0x0b}),
		)),
	))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func arm32MixedWidthModule(t *testing.T) *wasm.Module {
	t.Helper()
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I64}),
		)),
		wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{1})),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 1, 0x0b}),
			wasmtest.Code([]byte{0x42, 6, 0x42, 7, 0x7e, 0x0b}),
		)),
	))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCompileModuleAdmitsF64AndV128Functions(t *testing.T) {
	f64Body := append([]byte{0x44}, []byte{0, 0, 0, 0, 0, 0, 0xf0, 0xbf}...)
	f64Body = append(f64Body, 0x9a, 0x0b)
	v128Body := append([]byte{0xfd, 0x0c}, make([]byte, 16)...)
	v128Body = append(v128Body, 0x0b)
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.F64}), wasmtest.FuncType(nil, []wasm.ValType{wasm.V128}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{1})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(f64Body), wasmtest.Code(v128Body))),
	))
	if err != nil {
		t.Fatal(err)
	}
	cm, err := CompileModule(m)
	if err != nil {
		t.Fatal(err)
	}
	if cm.Functions[0].ResultSlots != 2 || cm.Functions[1].ResultSlots != 4 {
		t.Fatalf("metadata=%+v", cm.Functions)
	}
}

func TestCompileModuleLaysOutMixedWidthFunctions(t *testing.T) {
	cm, err := CompileModule(arm32MixedWidthModule(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(cm.Functions) != 2 || cm.Functions[0].ResultSlots != 1 || cm.Functions[1].ResultSlots != 2 {
		t.Fatalf("metadata=%+v", cm.Functions)
	}
	qemu, err := exec.LookPath("qemu-arm")
	if err != nil {
		return
	}
	meta := cm.Functions[1]
	fn := cm.Code[meta.Offset : meta.Offset+meta.Size]
	var wrapper a32.Asm
	call := wrapper.Call()
	armExit(&wrapper)
	wrapper.PatchCall(call, len(wrapper.B))
	runARM32Exit(t, qemu, append(wrapper.B, fn...), 42)
}

func TestCompileModuleLaysOutFunctions(t *testing.T) {
	cm, err := CompileModule(arm32Module(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(cm.Entry) != 2 || cm.Entry[0] != 0 || cm.Entry[1]%16 != 0 {
		t.Fatalf("entry=%v", cm.Entry)
	}
	if len(cm.Functions) != 2 || cm.Functions[1].Offset != uint32(cm.Entry[1]) || cm.Functions[1].Size == 0 {
		t.Fatalf("metadata=%+v", cm.Functions)
	}
}

func arm32LoadModule(t *testing.T) *wasm.Module {
	t.Helper()
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x2d, 0, 0, 0x0b}))),
	))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func arm32DivModule(t *testing.T) *wasm.Module {
	t.Helper()
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x6d, 0x0b}))),
	))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCompileModuleMemoryAndTrapContextUnderQEMU(t *testing.T) {
	qemu, err := exec.LookPath("qemu-arm")
	if err != nil {
		t.Skip("qemu-arm not installed")
	}
	cm, err := CompileModule(arm32LoadModule(t))
	if err != nil {
		t.Fatal(err)
	}
	fn := cm.Code[cm.Functions[0].Offset : cm.Functions[0].Offset+cm.Functions[0].Size]
	t.Run("load", func(t *testing.T) {
		var a a32.Asm
		armMemoryContext(&a)
		a.MovImm32(a32.R12, 42)
		a.Strb(a32.R12, a32.SP, 4)
		armContextArg(&a)
		a.MovReg(a32.R11, a32.R0)
		a.MovImm32(a32.R0, 4)
		call := a.Call()
		armExit(&a)
		a.PatchCall(call, len(a.B))
		runARM32Exit(t, qemu, append(a.B, fn...), 42)
	})
	t.Run("division-trap", func(t *testing.T) {
		div, err := CompileModule(arm32DivModule(t))
		if err != nil {
			t.Fatal(err)
		}
		meta := div.Functions[0]
		divFn := div.Code[meta.Offset : meta.Offset+meta.Size]
		var a a32.Asm
		armMemoryContext(&a)
		armContextArg(&a)
		a.MovReg(a32.R11, a32.R0)
		a.MovImm32(a32.R0, 1)
		a.MovImm32(a32.R1, 0)
		call := a.Call()
		a.Ldr(a32.R0, a32.SP, 32)
		armExit(&a)
		a.PatchCall(call, len(a.B))
		runARM32Exit(t, qemu, append(a.B, divFn...), int(embedded32.TrapIntegerDivideByZero))
	})
	t.Run("oob", func(t *testing.T) {
		var a a32.Asm
		armMemoryContext(&a)
		armContextArg(&a)
		a.MovReg(a32.R11, a32.R0)
		a.MovImm32(a32.R0, 16)
		call := a.Call()
		a.Ldr(a32.R0, a32.SP, 32)
		armExit(&a)
		a.PatchCall(call, len(a.B))
		runARM32Exit(t, qemu, append(a.B, fn...), int(embedded32.TrapMemoryOutOfBounds))
	})
}

func TestCompileModuleExecutesSelectedFunctionUnderQEMU(t *testing.T) {
	qemu, err := exec.LookPath("qemu-arm")
	if err != nil {
		t.Skip("qemu-arm not installed")
	}
	cm, err := CompileModule(arm32Module(t))
	if err != nil {
		t.Fatal(err)
	}
	meta := cm.Functions[1]
	fn := cm.Code[meta.Offset : meta.Offset+meta.Size]
	var wrapper a32.Asm
	wrapper.MovImm32(a32.R0, 41)
	call := wrapper.Call()
	armExit(&wrapper)
	if !wrapper.PatchCall(call, len(wrapper.B)) {
		t.Fatal("wrapper call patch rejected")
	}
	runARM32Exit(t, qemu, append(wrapper.B, fn...), 42)
}
