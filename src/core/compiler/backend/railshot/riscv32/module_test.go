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

func riscv32MixedWidthModule(t *testing.T) *wasm.Module {
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
	cm, err := CompileModule(riscv32MixedWidthModule(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(cm.Functions) != 2 || cm.Functions[0].ResultSlots != 1 || cm.Functions[1].ResultSlots != 2 {
		t.Fatalf("metadata=%+v", cm.Functions)
	}
	qemu, err := exec.LookPath("qemu-riscv32")
	if err != nil {
		return
	}
	meta := cm.Functions[1]
	fn := cm.Code[meta.Offset : meta.Offset+meta.Size]
	var wrapper rv.Asm
	call := wrapper.Jal(rv.RA)
	wrapper.MovImm32(rv.A7, 93)
	wrapper.Ecall()
	wrapper.PatchJAL21(call, len(wrapper.B))
	runRV32Exit(t, qemu, append(wrapper.B, fn...), 42)
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

func riscv32CallModule(t *testing.T, trapping bool) *wasm.Module {
	t.Helper()
	var types, funcs, code [][]byte
	if trapping {
		types = [][]byte{wasmtest.FuncType(nil, nil)}
		funcs = [][]byte{{0}, {0}}
		code = [][]byte{wasmtest.Code([]byte{0x00, 0x0b}), wasmtest.Code([]byte{0x10, 0, 0x0b})}
	} else {
		types = [][]byte{wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})}
		funcs = [][]byte{{0}, {0}}
		code = [][]byte{wasmtest.Code([]byte{0x41, 7, 0x0b}), wasmtest.Code([]byte{0x41, 35, 0x10, 0, 0x6a, 0x0b})}
	}
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(funcs...)),
		wasmtest.Section(10, wasmtest.Vec(code...)),
	))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func riscv32RecursiveModule(t *testing.T) *wasm.Module {
	t.Helper()
	recurBody := []byte{1, 1, 0x7f, 0x41, 1, 0x21, 1, 0x20, 0, 0x04, 0x40, 0x20, 0, 0x41, 1, 0x6b, 0x10, 0, 0x21, 1, 0x0b, 0x20, 1, 0x0b}
	recurCode := append(wasmtest.ULEB(uint32(len(recurBody))), recurBody...)
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{1})),
		wasmtest.Section(10, wasmtest.Vec(recurCode, wasmtest.Code([]byte{0x41, 3, 0x10, 0, 0x0b}))),
	))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCompileModuleDirectCallsUnderQEMU(t *testing.T) {
	qemu, err := exec.LookPath("qemu-riscv32")
	if err != nil {
		t.Skip("qemu-riscv32 not installed")
	}
	t.Run("recursion", func(t *testing.T) {
		cm, err := CompileModule(riscv32RecursiveModule(t))
		if err != nil {
			t.Fatal(err)
		}
		var a rv.Asm
		rvMemoryContext(&a)
		a.Addi(rv.A0, rv.SP, 16)
		a.MovReg(rv.X23, rv.A0)
		call := a.Jal(rv.RA)
		a.MovImm32(rv.A7, 93)
		a.Ecall()
		a.PatchJAL21(call, len(a.B)+cm.Entry[1])
		runRV32Exit(t, qemu, append(a.B, cm.Code...), 1)
	})
	for _, trapping := range []bool{false, true} {
		name := "live-value"
		if trapping {
			name = "nested-trap"
		}
		t.Run(name, func(t *testing.T) {
			cm, err := CompileModule(riscv32CallModule(t, trapping))
			if err != nil {
				t.Fatal(err)
			}
			var a rv.Asm
			rvMemoryContext(&a)
			a.Addi(rv.A0, rv.SP, 16)
			a.MovReg(rv.X23, rv.A0)
			call := a.Jal(rv.RA)
			if trapping {
				a.Lw(rv.A0, rv.SP, 32)
			}
			a.MovImm32(rv.A7, 93)
			a.Ecall()
			a.PatchJAL21(call, len(a.B)+cm.Entry[1])
			want := 42
			if trapping {
				want = int(embedded32.TrapUnreachable)
			}
			runRV32Exit(t, qemu, append(a.B, cm.Code...), want)
		})
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

func riscv32UnreachableModule(t *testing.T) *wasm.Module {
	t.Helper()
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x00, 0x0b}))),
	))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func riscv32GrowModule(t *testing.T) *wasm.Module {
	t.Helper()
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(5, wasmtest.Vec([]byte{1, 0, 1})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x40, 0, 0x0b}))),
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
	t.Run("unreachable", func(t *testing.T) {
		unreachable, err := CompileModule(riscv32UnreachableModule(t))
		if err != nil {
			t.Fatal(err)
		}
		meta := unreachable.Functions[0]
		unreachableFn := unreachable.Code[meta.Offset : meta.Offset+meta.Size]
		var a rv.Asm
		rvMemoryContext(&a)
		a.Addi(rv.A0, rv.SP, 16)
		a.MovReg(rv.X23, rv.A0)
		call := a.Jal(rv.RA)
		a.Lw(rv.A0, rv.SP, 32)
		a.MovImm32(rv.A7, 93)
		a.Ecall()
		a.PatchJAL21(call, len(a.B))
		runRV32Exit(t, qemu, append(a.B, unreachableFn...), int(embedded32.TrapUnreachable))
	})
	t.Run("canceled-entry", func(t *testing.T) {
		var a rv.Asm
		rvMemoryContext(&a)
		a.MovImm32(rv.T1, 1)
		a.Sw(rv.T1, rv.SP, 40)
		a.Addi(rv.A0, rv.SP, 16)
		a.MovReg(rv.X23, rv.A0)
		a.MovImm32(rv.A0, 4)
		call := a.Jal(rv.RA)
		a.Lw(rv.A0, rv.SP, 32)
		a.MovImm32(rv.A7, 93)
		a.Ecall()
		a.PatchJAL21(call, len(a.B))
		runRV32Exit(t, qemu, append(a.B, fn...), int(embedded32.TrapCanceled))
	})
	t.Run("memory-grow-failure", func(t *testing.T) {
		grow, err := CompileModule(riscv32GrowModule(t))
		if err != nil {
			t.Fatal(err)
		}
		meta := grow.Functions[0]
		growFn := grow.Code[meta.Offset : meta.Offset+meta.Size]
		var a rv.Asm
		rvMemoryContext(&a)
		a.MovImm32(rv.T1, 0)
		a.Sw(rv.T1, rv.SP, 20)
		a.Sw(rv.T1, rv.SP, 36)
		a.Addi(rv.A0, rv.SP, 16)
		a.MovReg(rv.X23, rv.A0)
		a.MovImm32(rv.A0, 1)
		call := a.Jal(rv.RA)
		a.MovImm32(rv.A7, 93)
		a.Ecall()
		a.PatchJAL21(call, len(a.B))
		runRV32Exit(t, qemu, append(a.B, growFn...), 255)
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
	rvMemoryContext(&wrapper)
	wrapper.Addi(rv.A0, rv.SP, 16)
	wrapper.MovReg(rv.X23, rv.A0)
	wrapper.MovImm32(rv.A0, 41)
	call := wrapper.Jal(rv.RA)
	wrapper.MovImm32(rv.A7, 93)
	wrapper.Ecall()
	if !wrapper.PatchJAL21(call, len(wrapper.B)) {
		t.Fatal("wrapper call patch rejected")
	}
	runRV32Exit(t, qemu, append(wrapper.B, fn...), 42)
}
