package riscv32

import (
	"os/exec"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	rv "github.com/wago-org/wago/src/core/encoder/riscv32"
	"github.com/wago-org/wago/src/core/runtime/embedded32"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func riscv32Module(t *testing.T) *wasm.Module {
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

func TestCompileModuleLaysOutFunctions(t *testing.T) {
	cm, err := CompileModule(riscv32Module(t))
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

func riscv32LoadModule(t *testing.T) *wasm.Module {
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

func riscv32DivModule(t *testing.T) *wasm.Module {
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
	qemu, err := exec.LookPath("qemu-riscv32")
	if err != nil {
		t.Skip("qemu-riscv32 not installed")
	}
	cm, err := CompileModule(riscv32LoadModule(t))
	if err != nil {
		t.Fatal(err)
	}
	fn := cm.Code[cm.Functions[0].Offset : cm.Functions[0].Offset+cm.Functions[0].Size]
	t.Run("load", func(t *testing.T) {
		var a rv.Asm
		rvMemoryContext(&a)
		a.MovImm32(rv.T1, 42)
		a.Sb(rv.T1, rv.SP, 4)
		a.Addi(rv.A0, rv.SP, 16)
		a.MovReg(rv.X23, rv.A0)
		a.MovImm32(rv.A0, 4)
		call := a.Jal(rv.RA)
		a.MovImm32(rv.A7, 93)
		a.Ecall()
		a.PatchJAL21(call, len(a.B))
		runRV32Exit(t, qemu, append(a.B, fn...), 42)
	})
	t.Run("division-trap", func(t *testing.T) {
		div, err := CompileModule(riscv32DivModule(t))
		if err != nil {
			t.Fatal(err)
		}
		meta := div.Functions[0]
		divFn := div.Code[meta.Offset : meta.Offset+meta.Size]
		var a rv.Asm
		rvMemoryContext(&a)
		a.Addi(rv.A0, rv.SP, 16)
		a.MovReg(rv.X23, rv.A0)
		a.MovImm32(rv.A0, 1)
		a.MovImm32(rv.A1, 0)
		call := a.Jal(rv.RA)
		a.Lw(rv.A0, rv.SP, 32)
		a.MovImm32(rv.A7, 93)
		a.Ecall()
		a.PatchJAL21(call, len(a.B))
		runRV32Exit(t, qemu, append(a.B, divFn...), int(embedded32.TrapIntegerDivideByZero))
	})
	t.Run("oob", func(t *testing.T) {
		var a rv.Asm
		rvMemoryContext(&a)
		a.Addi(rv.A0, rv.SP, 16)
		a.MovReg(rv.X23, rv.A0)
		a.MovImm32(rv.A0, 16)
		call := a.Jal(rv.RA)
		a.Lw(rv.A0, rv.SP, 32)
		a.MovImm32(rv.A7, 93)
		a.Ecall()
		a.PatchJAL21(call, len(a.B))
		runRV32Exit(t, qemu, append(a.B, fn...), int(embedded32.TrapMemoryOutOfBounds))
	})
}

func TestCompileModuleExecutesSelectedFunctionUnderQEMU(t *testing.T) {
	qemu, err := exec.LookPath("qemu-riscv32")
	if err != nil {
		t.Skip("qemu-riscv32 not installed")
	}
	cm, err := CompileModule(riscv32Module(t))
	if err != nil {
		t.Fatal(err)
	}
	meta := cm.Functions[1]
	fn := cm.Code[meta.Offset : meta.Offset+meta.Size]
	var wrapper rv.Asm
	wrapper.MovImm32(rv.A0, 41)
	call := wrapper.Jal(rv.RA)
	wrapper.MovImm32(rv.A7, 93)
	wrapper.Ecall()
	if !wrapper.PatchJAL21(call, len(wrapper.B)) {
		t.Fatal("wrapper call patch rejected")
	}
	runRV32Exit(t, qemu, append(wrapper.B, fn...), 42)
}
