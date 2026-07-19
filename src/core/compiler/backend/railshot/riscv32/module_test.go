package riscv32

import (
	"encoding/binary"
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

func TestCompileModuleUsesMixedPlannerForI32Signatures(t *testing.T) {
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0, 0x31, 0, 0, 0xa7, 0x0b}))),
	))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CompileModule(m); err != nil {
		t.Fatal(err)
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
	rvMemoryContext(&wrapper)
	wrapper.Addi(rv.A0, rv.SP, 16)
	wrapper.MovReg(rv.X23, rv.A0)
	call := wrapper.Jal(rv.RA)
	wrapper.MovImm32(rv.A7, 93)
	wrapper.Ecall()
	wrapper.PatchJAL21(call, len(wrapper.B))
	runRV32Exit(t, qemu, append(wrapper.B, fn...), 42)
}

func riscv32GenuinelyMixedModule(t *testing.T) *wasm.Module {
	t.Helper()
	body := []byte{3, 1, 0x7d, 1, 0x7c, 1, 0x7b,
		0x20, 1, 0x42, 5, 0x7c,
		0x43, 0x2a, 0, 0, 0x80, 0x8b, 0x1a,
		0x44, 0x2a, 0, 0, 0, 0, 0, 0, 0x80, 0x99, 0x1a,
		0xfd, 12, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 0xfd, 77, 0x1a,
		0x0b}
	code := append(wasmtest.ULEB(uint32(len(body))), body...)
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I64}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(10, wasmtest.Vec(code)),
	))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCompileModuleExecutesGenuinelyMixedFunctionUnderQEMU(t *testing.T) {
	qemu, err := exec.LookPath("qemu-riscv32")
	if err != nil {
		t.Skip("qemu-riscv32 not installed")
	}
	cm, err := CompileModule(riscv32GenuinelyMixedModule(t))
	if err != nil {
		t.Fatal(err)
	}
	if cm.Functions[0].ParamSlots != 3 || cm.Functions[0].ResultSlots != 2 {
		t.Fatalf("metadata=%+v", cm.Functions[0])
	}
	meta := cm.Functions[0]
	fn := cm.Code[meta.Offset : meta.Offset+meta.Size]
	var a rv.Asm
	rvMemoryContext(&a)
	a.Addi(rv.A0, rv.SP, 16)
	a.MovReg(rv.X23, rv.A0)
	a.MovImm32(rv.A0, 7)
	a.MovImm32(rv.A1, 37)
	a.MovImm32(rv.A2, 0)
	call := a.Jal(rv.RA)
	a.MovImm32(rv.A7, 93)
	a.Ecall()
	a.PatchJAL21(call, len(a.B))
	runRV32Exit(t, qemu, append(a.B, fn...), 42)
}

func riscv32MixedCallModule(t *testing.T) *wasm.Module {
	t.Helper()
	callee := []byte{0x20, 1, 0x42, 5, 0x7c, 0x0b}
	caller := []byte{0x42}
	caller = append(caller, wasmtest.SLEB64(100)...)
	caller = append(caller, 0x20, 0, 0x20, 1, 0x10, 0, 0x7c, 0x0b)
	sig := wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I64}, []wasm.ValType{wasm.I64})
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(sig)),
		wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{0})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(callee), wasmtest.Code(caller))),
	))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCompileModuleExecutesMixedCallWithLiveWideValueUnderQEMU(t *testing.T) {
	qemu, err := exec.LookPath("qemu-riscv32")
	if err != nil {
		t.Skip("qemu-riscv32 not installed")
	}
	cm, err := CompileModule(riscv32MixedCallModule(t))
	if err != nil {
		t.Fatal(err)
	}
	var a rv.Asm
	rvMemoryContext(&a)
	a.Addi(rv.A0, rv.SP, 16)
	a.MovReg(rv.X23, rv.A0)
	a.MovImm32(rv.A0, 7)
	a.MovImm32(rv.A1, 37)
	a.MovImm32(rv.A2, 0)
	call := a.Jal(rv.RA)
	a.MovImm32(rv.A7, 93)
	a.Ecall()
	if !a.PatchJAL21(call, len(a.B)+cm.Entry[1]) {
		t.Fatal("wrapper call relocation")
	}
	image := append(a.B, cm.Code...)
	runRV32Exit(t, qemu, image, 142)
}

func riscv32MixedTrapCallModule(t *testing.T) *wasm.Module {
	t.Helper()
	sig := wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I64}, []wasm.ValType{wasm.I64})
	caller := []byte{0x20, 0, 0x20, 1, 0x10, 0, 0x0b}
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(sig)),
		wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{0})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x00, 0x0b}), wasmtest.Code(caller))),
	))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCompileModulePropagatesMixedCallTrapUnderQEMU(t *testing.T) {
	qemu, err := exec.LookPath("qemu-riscv32")
	if err != nil {
		t.Skip("qemu-riscv32 not installed")
	}
	cm, err := CompileModule(riscv32MixedTrapCallModule(t))
	if err != nil {
		t.Fatal(err)
	}
	var a rv.Asm
	rvMemoryContext(&a)
	a.Addi(rv.A0, rv.SP, 16)
	a.MovReg(rv.X23, rv.A0)
	a.MovImm32(rv.A0, 7)
	a.MovImm32(rv.A1, 37)
	a.MovImm32(rv.A2, 0)
	call := a.Jal(rv.RA)
	a.Lw(rv.A0, rv.SP, 32)
	a.MovImm32(rv.A7, 93)
	a.Ecall()
	if !a.PatchJAL21(call, len(a.B)+cm.Entry[1]) {
		t.Fatal("wrapper call relocation")
	}
	runRV32Exit(t, qemu, append(a.B, cm.Code...), int(embedded32.TrapUnreachable))
}

func riscv32MixedMultiResultCallModule(t *testing.T) *wasm.Module {
	t.Helper()
	sig := wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I64}, []wasm.ValType{wasm.I32, wasm.I64})
	body := []byte{0x20, 0, 0x20, 1, 0x0b}
	caller := []byte{0x20, 0, 0x20, 1, 0x10, 0, 0x0b}
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(sig)),
		wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{0})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body), wasmtest.Code(caller))),
	))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCompileModuleReturnsMultipleMixedCallResultsUnderQEMU(t *testing.T) {
	qemu, err := exec.LookPath("qemu-riscv32")
	if err != nil {
		t.Skip("qemu-riscv32 not installed")
	}
	cm, err := CompileModule(riscv32MixedMultiResultCallModule(t))
	if err != nil {
		t.Fatal(err)
	}
	if cm.Functions[1].ResultSlots != 3 {
		t.Fatalf("metadata=%+v", cm.Functions[1])
	}
	var a rv.Asm
	rvMemoryContext(&a)
	a.Addi(rv.A0, rv.SP, 16)
	a.MovReg(rv.X23, rv.A0)
	a.MovImm32(rv.A0, 42)
	a.MovImm32(rv.A1, 37)
	a.MovImm32(rv.A2, 0)
	call := a.Jal(rv.RA)
	a.Add(rv.A0, rv.A0, rv.A1)
	a.MovImm32(rv.A7, 93)
	a.Ecall()
	if !a.PatchJAL21(call, len(a.B)+cm.Entry[1]) {
		t.Fatal("wrapper call relocation")
	}
	runRV32Exit(t, qemu, append(a.B, cm.Code...), 79)
}

func riscv32MixedSelectModule(t *testing.T) *wasm.Module {
	t.Helper()
	body := []byte{1, 1, 0x7d, 0x42, 37, 0x42, 42, 0x41, 0, 0x1c, 1, 0x7e, 0x0f, 0x0b}
	code := append(wasmtest.ULEB(uint32(len(body))), body...)
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(10, wasmtest.Vec(code)),
	))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCompileModuleSelectsAtomicWideValueUnderQEMU(t *testing.T) {
	qemu, err := exec.LookPath("qemu-riscv32")
	if err != nil {
		t.Skip("qemu-riscv32 not installed")
	}
	cm, err := CompileModule(riscv32MixedSelectModule(t))
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
	if !a.PatchJAL21(call, len(a.B)+cm.Entry[0]) {
		t.Fatal("wrapper call relocation")
	}
	runRV32Exit(t, qemu, append(a.B, cm.Code...), 42)
}

func riscv32MixedStackABIModule(t *testing.T) *wasm.Module {
	t.Helper()
	sig := wasmtest.FuncType(
		[]wasm.ValType{wasm.V128, wasm.V128, wasm.I32},
		[]wasm.ValType{wasm.V128, wasm.V128, wasm.I32},
	)
	callee := []byte{0x20, 0, 0x20, 1, 0x20, 2, 0x0b}
	caller := []byte{0x20, 0, 0x20, 1, 0x20, 2, 0x10, 0, 0x0b}
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(sig)),
		wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{0})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(callee), wasmtest.Code(caller))),
	))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCompileModuleUsesMixedStackArgumentsAndResultsUnderQEMU(t *testing.T) {
	qemu, err := exec.LookPath("qemu-riscv32")
	if err != nil {
		t.Skip("qemu-riscv32 not installed")
	}
	cm, err := CompileModule(riscv32MixedStackABIModule(t))
	if err != nil {
		t.Fatal(err)
	}
	if cm.Functions[1].ParamSlots != 9 || cm.Functions[1].ResultSlots != 9 {
		t.Fatalf("metadata=%+v", cm.Functions[1])
	}
	var a rv.Asm
	rvMemoryContext(&a)
	a.MovImm32(rv.T0, 12)
	a.Sw(rv.T0, rv.SP, 0)
	a.Addi(rv.A0, rv.SP, 16)
	a.MovReg(rv.X23, rv.A0)
	a.MovImm32(rv.A0, 10)
	a.MovImm32(rv.A1, 0)
	a.MovImm32(rv.A2, 0)
	a.MovImm32(rv.A3, 0)
	a.MovImm32(rv.A4, 20)
	a.MovImm32(rv.A5, 0)
	a.MovImm32(rv.A6, 0)
	a.MovImm32(rv.A7, 0)
	call := a.Jal(rv.RA)
	a.Add(rv.A0, rv.A0, rv.A4)
	a.Lw(rv.T0, rv.SP, 0)
	a.Add(rv.A0, rv.A0, rv.T0)
	a.MovImm32(rv.A7, 93)
	a.Ecall()
	if !a.PatchJAL21(call, len(a.B)+cm.Entry[1]) {
		t.Fatal("wrapper call relocation")
	}
	runRV32Exit(t, qemu, append(a.B, cm.Code...), 42)
}

func riscv32MixedIfModule(t *testing.T) *wasm.Module {
	t.Helper()
	body := []byte{2, 1, 0x7e, 1, 0x7d,
		0x42, 40, 0x41, 0, 0x04, 0x40,
		0x42, 1, 0x21, 0,
		0x05, 0x42, 2, 0x21, 0,
		0x0b, 0x20, 0, 0x7c, 0x0b}
	code := append(wasmtest.ULEB(uint32(len(body))), body...)
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(10, wasmtest.Vec(code)),
	))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCompileModulePreservesWideValueAcrossMixedIfUnderQEMU(t *testing.T) {
	qemu, err := exec.LookPath("qemu-riscv32")
	if err != nil {
		t.Skip("qemu-riscv32 not installed")
	}
	cm, err := CompileModule(riscv32MixedIfModule(t))
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
	if !a.PatchJAL21(call, len(a.B)+cm.Entry[0]) {
		t.Fatal("wrapper call relocation")
	}
	runRV32Exit(t, qemu, append(a.B, cm.Code...), 42)
}

func riscv32MixedLoopModule(t *testing.T) *wasm.Module {
	t.Helper()
	body := []byte{2, 1, 0x7f, 1, 0x7e,
		0x41, 3, 0x21, 0,
		0x42, 40, 0x21, 1,
		0x03, 0x40,
		0x20, 1, 0x42, 1, 0x7c, 0x21, 1,
		0x20, 0, 0x41, 1, 0x6b, 0x22, 0, 0x0d, 0,
		0x0b, 0x20, 1, 0x0b}
	code := append(wasmtest.ULEB(uint32(len(body))), body...)
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(10, wasmtest.Vec(code)),
	))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCompileModuleExecutesMixedLoopWithWideLocalUnderQEMU(t *testing.T) {
	qemu, err := exec.LookPath("qemu-riscv32")
	if err != nil {
		t.Skip("qemu-riscv32 not installed")
	}
	cm, err := CompileModule(riscv32MixedLoopModule(t))
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
	if !a.PatchJAL21(call, len(a.B)+cm.Entry[0]) {
		t.Fatal("wrapper call relocation")
	}
	runRV32Exit(t, qemu, append(a.B, cm.Code...), 43)
}

func riscv32MixedGlobalModule(t *testing.T) *wasm.Module {
	t.Helper()
	body := []byte{1, 1, 0x7c, 0x23, 0, 0x41, 2, 0x6a, 0x24, 0, 0x23, 0, 0x0b}
	code := append(wasmtest.ULEB(uint32(len(body))), body...)
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 40, 0x0b}))),
		wasmtest.Section(10, wasmtest.Vec(code)),
	))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCompileModuleAccessesI32GlobalFromMixedFunctionUnderQEMU(t *testing.T) {
	qemu, err := exec.LookPath("qemu-riscv32")
	if err != nil {
		t.Skip("qemu-riscv32 not installed")
	}
	cm, err := CompileModule(riscv32MixedGlobalModule(t))
	if err != nil {
		t.Fatal(err)
	}
	var a rv.Asm
	rvMemoryContext(&a)
	a.MovImm32(rv.T0, 40)
	a.Sw(rv.T0, rv.SP, 60)
	a.Addi(rv.A0, rv.SP, 16)
	a.MovReg(rv.X23, rv.A0)
	call := a.Jal(rv.RA)
	a.MovImm32(rv.A7, 93)
	a.Ecall()
	if !a.PatchJAL21(call, len(a.B)+cm.Entry[0]) {
		t.Fatal("wrapper call relocation")
	}
	runRV32Exit(t, qemu, append(a.B, cm.Code...), 42)
}

func riscv32MixedMemorySizeGrowModule(t *testing.T) *wasm.Module {
	t.Helper()
	body := []byte{1, 1, 0x7c, 0x41, 0, 0x40, 0, 0x3f, 0, 0x6a, 0x0b}
	code := append(wasmtest.ULEB(uint32(len(body))), body...)
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(5, wasmtest.Vec([]byte{1, 1, 1})),
		wasmtest.Section(10, wasmtest.Vec(code)),
	))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCompileModuleExecutesMemorySizeGrowFromMixedFunctionUnderQEMU(t *testing.T) {
	qemu, err := exec.LookPath("qemu-riscv32")
	if err != nil {
		t.Skip("qemu-riscv32 not installed")
	}
	cm, err := CompileModule(riscv32MixedMemorySizeGrowModule(t))
	if err != nil {
		t.Fatal(err)
	}
	var a rv.Asm
	rvMemoryContext(&a)
	a.MovImm32(rv.T0, 65536)
	a.Sw(rv.T0, rv.SP, 20)
	a.Sw(rv.T0, rv.SP, 36)
	a.Addi(rv.A0, rv.SP, 16)
	a.MovReg(rv.X23, rv.A0)
	call := a.Jal(rv.RA)
	a.MovImm32(rv.A7, 93)
	a.Ecall()
	if !a.PatchJAL21(call, len(a.B)+cm.Entry[0]) {
		t.Fatal("wrapper call relocation")
	}
	runRV32Exit(t, qemu, append(a.B, cm.Code...), 2)
}

func riscv32WideGlobalModule(t *testing.T) *wasm.Module {
	t.Helper()
	init := append([]byte{0x42}, wasmtest.SLEB64(40)...)
	init = append(init, 0x0b)
	body := append([]byte{0x23, 0, 0x42}, wasmtest.SLEB64(2)...)
	body = append(body, 0x7c, 0x24, 0, 0x23, 0, 0x0b)
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I64, true, init))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCompileModuleAccessesWideGlobalUnderQEMU(t *testing.T) {
	qemu, err := exec.LookPath("qemu-riscv32")
	if err != nil {
		t.Skip("qemu-riscv32 not installed")
	}
	cm, err := CompileModule(riscv32WideGlobalModule(t))
	if err != nil {
		t.Fatal(err)
	}
	cells := make([]uint32, 2)
	if err := cm.InstantiateGlobals(cells); err != nil || cells[0] != 40 || cells[1] != 0 {
		t.Fatalf("globals=%v err=%v", cells, err)
	}
	var a rv.Asm
	rvMemoryContext(&a)
	a.Addi(rv.T0, rv.SP, 72)
	a.Sw(rv.T0, rv.SP, 44)
	a.MovImm32(rv.T0, cells[0])
	a.Sw(rv.T0, rv.SP, 72)
	a.MovImm32(rv.T0, cells[1])
	a.Sw(rv.T0, rv.SP, 76)
	a.Addi(rv.A0, rv.SP, 16)
	a.MovReg(rv.X23, rv.A0)
	call := a.Jal(rv.RA)
	a.MovImm32(rv.A7, 93)
	a.Ecall()
	if !a.PatchJAL21(call, len(a.B)+cm.Entry[0]) {
		t.Fatal("wrapper call relocation")
	}
	runRV32Exit(t, qemu, append(a.B, cm.Code...), 42)
}

func riscv32MixedI32CompleteModule(t *testing.T) *wasm.Module {
	t.Helper()
	body := []byte{1, 1, 0x7c,
		0x41, 16, 0x67,
		0x41, 16, 0x68, 0x6a,
		0x41, 15, 0x69, 0x6a,
		0x41, 42, 0x41, 2, 0x6e, 0x6a,
		0x41, 1, 0x41, 33, 0x74, 0x6a,
		0x41, 0x7f, 0xc0, 0x6a,
		0x42, 42, 0xa7, 0x6a,
		0x0b,
	}
	code := append(wasmtest.ULEB(uint32(len(body))), body...)
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(10, wasmtest.Vec(code)),
	))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCompileModuleExecutesCompleteMixedI32SemanticsUnderQEMU(t *testing.T) {
	qemu, err := exec.LookPath("qemu-riscv32")
	if err != nil {
		t.Skip("qemu-riscv32 not installed")
	}
	cm, err := CompileModule(riscv32MixedI32CompleteModule(t))
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
	if !a.PatchJAL21(call, len(a.B)+cm.Entry[0]) {
		t.Fatal("wrapper call relocation")
	}
	runRV32Exit(t, qemu, append(a.B, cm.Code...), 99)
}

func riscv32MixedI32CompareModule(t *testing.T) *wasm.Module {
	t.Helper()
	body := []byte{1, 1, 0x7c,
		0x41, 0x7f, 0x41, 1, 0x48,
		0x41, 0x7f, 0x41, 1, 0x4b, 0x6a,
		0x41, 0, 0x45, 0x6a,
		0x41, 5, 0x41, 5, 0x46, 0x6a,
		0x0b,
	}
	code := append(wasmtest.ULEB(uint32(len(body))), body...)
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(10, wasmtest.Vec(code)),
	))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCompileModuleExecutesMixedI32ComparisonsUnderQEMU(t *testing.T) {
	qemu, err := exec.LookPath("qemu-riscv32")
	if err != nil {
		t.Skip("qemu-riscv32 not installed")
	}
	cm, err := CompileModule(riscv32MixedI32CompareModule(t))
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
	if !a.PatchJAL21(call, len(a.B)+cm.Entry[0]) {
		t.Fatal("wrapper call relocation")
	}
	runRV32Exit(t, qemu, append(a.B, cm.Code...), 4)
}

func riscv32ReferenceModule(t *testing.T) *wasm.Module {
	t.Helper()
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.FuncRef}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0xd1, 0x0b}))),
	))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCompileModuleExecutesReferenceValuesUnderQEMU(t *testing.T) {
	qemu, err := exec.LookPath("qemu-riscv32")
	if err != nil {
		t.Skip("qemu-riscv32 not installed")
	}
	cm, err := CompileModule(riscv32ReferenceModule(t))
	if err != nil {
		t.Fatal(err)
	}
	var a rv.Asm
	rvMemoryContext(&a)
	a.Addi(rv.A0, rv.SP, 16)
	a.MovReg(rv.X23, rv.A0)
	a.MovImm32(rv.A0, 0)
	first := a.Jal(rv.RA)
	a.MovReg(rv.S0, rv.A0)
	a.MovImm32(rv.A0, 7)
	second := a.Jal(rv.RA)
	a.Add(rv.A0, rv.A0, rv.S0)
	a.MovImm32(rv.A7, 93)
	a.Ecall()
	target := len(a.B) + cm.Entry[0]
	if !a.PatchJAL21(first, target) || !a.PatchJAL21(second, target) {
		t.Fatal("wrapper call relocation")
	}
	runRV32Exit(t, qemu, append(a.B, cm.Code...), 1)
}

func TestCompileModuleExecutesRefNullUnderQEMU(t *testing.T) {
	qemu, err := exec.LookPath("qemu-riscv32")
	if err != nil {
		t.Skip("qemu-riscv32 not installed")
	}
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(10, wasmtest.Vec([]byte{7, 1, 1, 0x7c, 0xd0, 0x70, 0xd1, 0x0b})),
	))
	if err != nil {
		t.Fatal(err)
	}
	cm, err := CompileModule(m)
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
	if !a.PatchJAL21(call, len(a.B)+cm.Entry[0]) {
		t.Fatal("wrapper call relocation")
	}
	runRV32Exit(t, qemu, append(a.B, cm.Code...), 1)
}

func riscv32TableAccessModule(t *testing.T) *wasm.Module {
	t.Helper()
	body := []byte{0x41, 0, 0x20, 0, 0x26, 0, 0x41, 0, 0x25, 0, 0xd1, 0x41, 42, 0x6a, 0x0b}
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.FuncRef}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 1, 2, 2})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCompileModuleAccessesFunctionTableUnderQEMU(t *testing.T) {
	qemu, err := exec.LookPath("qemu-riscv32")
	if err != nil {
		t.Skip("qemu-riscv32 not installed")
	}
	cm, err := CompileModule(riscv32TableAccessModule(t))
	if err != nil {
		t.Fatal(err)
	}
	entries := make([]uint32, 2)
	if err := cm.InstantiateTable(entries); err != nil {
		t.Fatal(err)
	}
	var a rv.Asm
	rvMemoryContext(&a)
	a.Addi(rv.T0, rv.SP, 92)
	a.Sw(rv.T0, rv.SP, 28)
	a.Addi(rv.T0, rv.SP, 64)
	a.Sw(rv.T0, rv.SP, 56)
	a.Addi(rv.T0, rv.SP, 96)
	a.Sw(rv.T0, rv.SP, 64)
	a.MovImm32(rv.T0, 2)
	a.Sw(rv.T0, rv.SP, 68)
	a.Sw(rv.T0, rv.SP, 72)
	a.Sw(rv.Zero, rv.SP, 76)
	a.Sw(rv.Zero, rv.SP, 80)
	a.Sw(rv.Zero, rv.SP, 84)
	a.Sw(rv.Zero, rv.SP, 88)
	a.Sw(rv.Zero, rv.SP, 92)
	a.Sw(rv.Zero, rv.SP, 96)
	a.Sw(rv.Zero, rv.SP, 100)
	a.Addi(rv.A0, rv.SP, 16)
	a.MovReg(rv.X23, rv.A0)
	a.MovImm32(rv.A0, 7)
	call := a.Jal(rv.RA)
	a.MovImm32(rv.A7, 93)
	a.Ecall()
	if !a.PatchJAL21(call, len(a.B)+cm.Entry[0]) {
		t.Fatal("wrapper call relocation")
	}
	runRV32Exit(t, qemu, append(a.B, cm.Code...), 42)
}

func riscv32TableBulkModule(t *testing.T) *wasm.Module {
	t.Helper()
	body := []byte{
		0x20, 0, 0x41, 2, 0xfc, 15, 0,
		0xfc, 16, 0, 0x6a,
		0x41, 0, 0x20, 0, 0x41, 2, 0xfc, 17, 0,
		0x41, 2, 0x41, 0, 0x41, 2, 0xfc, 14, 0, 0,
		0x41, 3, 0x25, 0, 0xd1, 0x6a, 0x0b,
	}
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.FuncRef}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 1, 2, 4})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCompileModuleGrowsAndCopiesFunctionTableUnderQEMU(t *testing.T) {
	qemu, err := exec.LookPath("qemu-riscv32")
	if err != nil {
		t.Skip("qemu-riscv32 not installed")
	}
	cm, err := CompileModule(riscv32TableBulkModule(t))
	if err != nil {
		t.Fatal(err)
	}
	var a rv.Asm
	rvMemoryContext(&a)
	a.Addi(rv.T0, rv.SP, 124)
	a.Sw(rv.T0, rv.SP, 28)
	a.Addi(rv.T0, rv.SP, 64)
	a.Sw(rv.T0, rv.SP, 56)
	a.Addi(rv.T0, rv.SP, 96)
	a.Sw(rv.T0, rv.SP, 64)
	a.MovImm32(rv.T0, 2)
	a.Sw(rv.T0, rv.SP, 68)
	a.MovImm32(rv.T0, 4)
	a.Sw(rv.T0, rv.SP, 72)
	a.Sw(rv.Zero, rv.SP, 76)
	a.Sw(rv.Zero, rv.SP, 80)
	a.Sw(rv.Zero, rv.SP, 84)
	a.Sw(rv.Zero, rv.SP, 88)
	a.Sw(rv.Zero, rv.SP, 92)
	a.Sw(rv.Zero, rv.SP, 96)
	a.Sw(rv.Zero, rv.SP, 100)
	a.Sw(rv.Zero, rv.SP, 104)
	a.Sw(rv.Zero, rv.SP, 108)
	a.Sw(rv.Zero, rv.SP, 124)
	a.Addi(rv.A0, rv.SP, 16)
	a.MovReg(rv.X23, rv.A0)
	a.MovImm32(rv.A0, 7)
	call := a.Jal(rv.RA)
	a.MovImm32(rv.A7, 93)
	a.Ecall()
	if !a.PatchJAL21(call, len(a.B)+cm.Entry[0]) {
		t.Fatal("wrapper call relocation")
	}
	runRV32Exit(t, qemu, append(a.B, cm.Code...), 6)
}

func riscv32TableInitModule(t *testing.T) *wasm.Module {
	t.Helper()
	callerBody := []byte{1, 1, 0x7c,
		0x41, 0, 0x41, 0, 0x41, 1, 0xfc, 12, 0, 0,
		0xfc, 13, 0,
		0x41, 0, 0x25, 0, 0xd1, 0x41, 42, 0x6a, 0x0b,
	}
	callerCode := append(wasmtest.ULEB(uint32(len(callerBody))), callerBody...)
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{0})),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 1, 1, 1})),
		wasmtest.Section(9, wasmtest.Vec([]byte{1, 0, 1, 0})),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 42, 0x0b}),
			callerCode,
		)),
	))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCompileModuleInitializesTableFromPassiveElementUnderQEMU(t *testing.T) {
	qemu, err := exec.LookPath("qemu-riscv32")
	if err != nil {
		t.Skip("qemu-riscv32 not installed")
	}
	cm, err := CompileModule(riscv32TableInitModule(t))
	if err != nil {
		t.Fatal(err)
	}
	segments, err := cm.ElementSegmentABI([]uint32{0x1234})
	if err != nil || len(segments) != 1 || segments[0].Dropped != 0 || segments[0].Length != 1 {
		t.Fatalf("element segments=%v err=%v", segments, err)
	}
	var a rv.Asm
	rvMemoryContext(&a)
	a.Addi(rv.T0, rv.SP, 124)
	a.Sw(rv.T0, rv.SP, 28)
	a.Addi(rv.T0, rv.SP, 64)
	a.Sw(rv.T0, rv.SP, 56)
	a.Addi(rv.T0, rv.SP, 96)
	a.Sw(rv.T0, rv.SP, 64)
	a.MovImm32(rv.T0, 1)
	a.Sw(rv.T0, rv.SP, 68)
	a.Sw(rv.T0, rv.SP, 72)
	a.Sw(rv.Zero, rv.SP, 76)
	a.Sw(rv.Zero, rv.SP, 80)
	a.Addi(rv.T0, rv.SP, 104)
	a.Sw(rv.T0, rv.SP, 84)
	a.MovImm32(rv.T0, 1)
	a.Sw(rv.T0, rv.SP, 88)
	a.Sw(rv.Zero, rv.SP, 96)
	a.Addi(rv.T0, rv.SP, 120)
	a.Sw(rv.T0, rv.SP, 104)
	a.MovImm32(rv.T0, 1)
	a.Sw(rv.T0, rv.SP, 108)
	a.Sw(rv.Zero, rv.SP, 112)
	a.MovImm32(rv.T0, 1)
	a.Sw(rv.T0, rv.SP, 120)
	a.Sw(rv.Zero, rv.SP, 124)
	a.Addi(rv.A0, rv.SP, 16)
	a.MovReg(rv.X23, rv.A0)
	call := a.Jal(rv.RA)
	a.MovImm32(rv.A7, 93)
	a.Ecall()
	if !a.PatchJAL21(call, len(a.B)+cm.Entry[1]) {
		t.Fatal("wrapper call relocation")
	}
	runRV32Exit(t, qemu, append(a.B, cm.Code...), 42)
}

func riscv32IndirectCallModule(t *testing.T) *wasm.Module {
	t.Helper()
	callerBody := []byte{1, 1, 0x7c, 0x20, 0, 0x41, 0, 0x11, 0, 0, 0x0b}
	callerCode := append(wasmtest.ULEB(uint32(len(callerBody))), callerBody...)
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{1})),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 1, 1, 1})),
		wasmtest.Section(9, wasmtest.Vec([]byte{0, 0x41, 0, 0x0b, 1, 0})),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0x41, 1, 0x6a, 0x0b}),
			callerCode,
		)),
	))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCompileModuleCallsFunctionTableUnderQEMU(t *testing.T) {
	qemu, err := exec.LookPath("qemu-riscv32")
	if err != nil {
		t.Skip("qemu-riscv32 not installed")
	}
	cm, err := CompileModule(riscv32IndirectCallModule(t))
	if err != nil {
		t.Fatal(err)
	}
	entries := make([]uint32, 1)
	if err := cm.InstantiateTable(entries); err != nil || entries[0] != 1 {
		t.Fatalf("entries=%v err=%v", entries, err)
	}
	if len(cm.FunctionTypeIDs) != 2 || cm.FunctionTypeIDs[0] != cm.FunctionTypeIDs[1] {
		t.Fatalf("function type IDs=%v", cm.FunctionTypeIDs)
	}
	const base = uint32(0x10000)
	buildWrapper := func(fn0, fn1 uint32) rv.Asm {
		var a rv.Asm
		rvMemoryContext(&a)
		a.Addi(rv.T0, rv.SP, 124)
		a.Sw(rv.T0, rv.SP, 28)
		a.Addi(rv.T0, rv.SP, 64)
		a.Sw(rv.T0, rv.SP, 56)
		a.Addi(rv.T0, rv.SP, 96)
		a.Sw(rv.T0, rv.SP, 64)
		a.MovImm32(rv.T0, 1)
		a.Sw(rv.T0, rv.SP, 68)
		a.Sw(rv.T0, rv.SP, 72)
		a.Addi(rv.T0, rv.SP, 100)
		a.Sw(rv.T0, rv.SP, 76)
		a.Addi(rv.T0, rv.SP, 112)
		a.Sw(rv.T0, rv.SP, 80)
		a.Sw(rv.Zero, rv.SP, 84)
		a.Sw(rv.Zero, rv.SP, 88)
		a.MovImm32(rv.T0, entries[0])
		a.Sw(rv.T0, rv.SP, 96)
		a.MovImm32(rv.T0, fn0)
		a.Sw(rv.T0, rv.SP, 100)
		a.MovImm32(rv.T0, fn1)
		a.Sw(rv.T0, rv.SP, 104)
		a.MovImm32(rv.T0, cm.FunctionTypeIDs[0])
		a.Sw(rv.T0, rv.SP, 112)
		a.MovImm32(rv.T0, cm.FunctionTypeIDs[1])
		a.Sw(rv.T0, rv.SP, 116)
		a.Sw(rv.Zero, rv.SP, 124)
		a.Addi(rv.A0, rv.SP, 16)
		a.MovReg(rv.X23, rv.A0)
		a.MovImm32(rv.A0, 41)
		call := a.Jal(rv.RA)
		a.MovImm32(rv.A7, 93)
		a.Ecall()
		if !a.PatchJAL21(call, len(a.B)+cm.Entry[1]) {
			t.Fatal("wrapper call relocation")
		}
		return a
	}
	wrapper := buildWrapper(base, base)
	for {
		fn0 := base + uint32(len(wrapper.B)+cm.Entry[0])
		fn1 := base + uint32(len(wrapper.B)+cm.Entry[1])
		next := buildWrapper(fn0, fn1)
		if len(next.B) == len(wrapper.B) {
			wrapper = next
			break
		}
		wrapper = next
	}
	runRV32Exit(t, qemu, append(wrapper.B, cm.Code...), 42)
}

func riscv32MixedF64HelperModule(t *testing.T) *wasm.Module {
	t.Helper()
	body := []byte{1, 1, 0x7f,
		0x44, 20, 0, 0, 0, 0, 0, 0, 0,
		0x44, 22, 0, 0, 0, 0, 0, 0, 0,
		0xa0, 0x0b}
	code := append(wasmtest.ULEB(uint32(len(body))), body...)
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.F64}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(10, wasmtest.Vec(code)),
	))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCompileModuleDispatchesF64HelperFromMixedFunctionUnderQEMU(t *testing.T) {
	qemu, err := exec.LookPath("qemu-riscv32")
	if err != nil {
		t.Skip("qemu-riscv32 not installed")
	}
	cm, err := CompileModule(riscv32MixedF64HelperModule(t))
	if err != nil {
		t.Fatal(err)
	}
	var helper rv.Asm
	helper.Lw(rv.T0, rv.A0, embedded32.F64FrameALoOffset)
	helper.Lw(rv.T1, rv.A0, embedded32.F64FrameBLoOffset)
	helper.Add(rv.T0, rv.T0, rv.T1)
	helper.Sw(rv.T0, rv.A0, embedded32.F64FrameOutLoOffset)
	helper.Sw(rv.Zero, rv.A0, embedded32.F64FrameOutHiOffset)
	helper.Sw(rv.Zero, rv.A0, embedded32.F64FrameTrapOffset)
	helper.Ret()
	buildWrapper := func(table uint32) rv.Asm {
		var a rv.Asm
		rvMemoryContext(&a)
		a.Addi(rv.T0, rv.SP, 84)
		a.Sw(rv.T0, rv.SP, 24)
		a.Sw(rv.Zero, rv.SP, 84)
		a.MovImm32(rv.T0, table)
		a.Sw(rv.T0, rv.SP, 32)
		a.Addi(rv.A0, rv.SP, 16)
		a.MovReg(rv.X23, rv.A0)
		call := a.Jal(rv.RA)
		a.MovImm32(rv.A7, 93)
		a.Ecall()
		if table != 0 && !a.PatchJAL21(call, len(a.B)+cm.Entry[0]) {
			t.Fatal("wrapper call relocation")
		}
		return a
	}
	const base = uint32(0x10000)
	wrapper := buildWrapper(base)
	for {
		helperOff := len(wrapper.B) + len(cm.Code)
		tableOff := helperOff + len(helper.B)
		next := buildWrapper(base + uint32(tableOff))
		if len(next.B) == len(wrapper.B) {
			wrapper = next
			break
		}
		wrapper = next
	}
	helperOff := len(wrapper.B) + len(cm.Code)
	image := append(wrapper.B, cm.Code...)
	image = append(image, helper.B...)
	var table [embedded32.HelperTableBytes]byte
	binary.LittleEndian.PutUint32(table[embedded32.HelperF64Offset:], base+uint32(helperOff))
	image = append(image, table[:]...)
	runRV32Exit(t, qemu, image, 42)
}

func riscv32MixedI64MultiplyModule(t *testing.T) *wasm.Module {
	t.Helper()
	body := []byte{1, 1, 0x7d, 0x42, 6, 0x42, 7, 0x7e, 0x0b}
	code := append(wasmtest.ULEB(uint32(len(body))), body...)
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(10, wasmtest.Vec(code)),
	))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCompileModuleMultipliesI64InMixedFunctionUnderQEMU(t *testing.T) {
	qemu, err := exec.LookPath("qemu-riscv32")
	if err != nil {
		t.Skip("qemu-riscv32 not installed")
	}
	cm, err := CompileModule(riscv32MixedI64MultiplyModule(t))
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
	if !a.PatchJAL21(call, len(a.B)+cm.Entry[0]) {
		t.Fatal("wrapper call relocation")
	}
	runRV32Exit(t, qemu, append(a.B, cm.Code...), 42)
}

func riscv32MixedI64HelperModule(t *testing.T) *wasm.Module {
	t.Helper()
	body := []byte{1, 1, 0x7d, 0x42, 40, 0x42, 2, 0x80, 0x0b}
	code := append(wasmtest.ULEB(uint32(len(body))), body...)
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(10, wasmtest.Vec(code)),
	))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCompileModuleDispatchesI64HelperFromMixedFunctionUnderQEMU(t *testing.T) {
	qemu, err := exec.LookPath("qemu-riscv32")
	if err != nil {
		t.Skip("qemu-riscv32 not installed")
	}
	cm, err := CompileModule(riscv32MixedI64HelperModule(t))
	if err != nil {
		t.Fatal(err)
	}
	var helper rv.Asm
	helper.Lw(rv.T0, rv.A0, embedded32.I64FrameALoOffset)
	helper.Lw(rv.T1, rv.A0, embedded32.I64FrameBLoOffset)
	helper.Add(rv.T0, rv.T0, rv.T1)
	helper.Sw(rv.T0, rv.A0, embedded32.I64FrameOutLoOffset)
	helper.Sw(rv.Zero, rv.A0, embedded32.I64FrameOutHiOffset)
	helper.Sw(rv.Zero, rv.A0, embedded32.I64FrameTrapOffset)
	helper.Ret()
	buildWrapper := func(table uint32) rv.Asm {
		var a rv.Asm
		rvMemoryContext(&a)
		a.Addi(rv.T0, rv.SP, 84)
		a.Sw(rv.T0, rv.SP, 24)
		a.Sw(rv.Zero, rv.SP, 84)
		a.MovImm32(rv.T0, table)
		a.Sw(rv.T0, rv.SP, 32)
		a.Addi(rv.A0, rv.SP, 16)
		a.MovReg(rv.X23, rv.A0)
		call := a.Jal(rv.RA)
		a.MovImm32(rv.A7, 93)
		a.Ecall()
		if !a.PatchJAL21(call, len(a.B)+cm.Entry[0]) {
			t.Fatal("wrapper call relocation")
		}
		return a
	}
	const base = uint32(0x10000)
	wrapper := buildWrapper(base)
	for {
		helperOff := len(wrapper.B) + len(cm.Code)
		tableOff := helperOff + len(helper.B)
		next := buildWrapper(base + uint32(tableOff))
		if len(next.B) == len(wrapper.B) {
			wrapper = next
			break
		}
		wrapper = next
	}
	helperOff := len(wrapper.B) + len(cm.Code)
	image := append(wrapper.B, cm.Code...)
	image = append(image, helper.B...)
	var table [embedded32.HelperTableBytes]byte
	binary.LittleEndian.PutUint32(table[embedded32.HelperI64Offset:], base+uint32(helperOff))
	image = append(image, table[:]...)
	runRV32Exit(t, qemu, image, 42)
}

func riscv32MixedMemoryModule(t *testing.T) *wasm.Module {
	t.Helper()
	body := []byte{1, 1, 0x7d,
		0x41, 0, 0x42, 42, 0x37, 3, 0,
		0x41, 0, 0x29, 3, 0, 0x0b}
	code := append(wasmtest.ULEB(uint32(len(body))), body...)
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})),
		wasmtest.Section(10, wasmtest.Vec(code)),
	))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCompileModuleLoadsAndStoresI64FromMixedFunctionUnderQEMU(t *testing.T) {
	qemu, err := exec.LookPath("qemu-riscv32")
	if err != nil {
		t.Skip("qemu-riscv32 not installed")
	}
	cm, err := CompileModule(riscv32MixedMemoryModule(t))
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
	if !a.PatchJAL21(call, len(a.B)+cm.Entry[0]) {
		t.Fatal("wrapper call relocation")
	}
	runRV32Exit(t, qemu, append(a.B, cm.Code...), 42)
}

func riscv32MixedTypedIfModule(t *testing.T) *wasm.Module {
	t.Helper()
	body := []byte{1, 1, 0x7d,
		0x41, 0, 0x04, 0x7e,
		0x42, 37, 0x05, 0x42, 42, 0x0b, 0x0b}
	code := append(wasmtest.ULEB(uint32(len(body))), body...)
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(10, wasmtest.Vec(code)),
	))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCompileModuleMergesTypedWideIfResultUnderQEMU(t *testing.T) {
	qemu, err := exec.LookPath("qemu-riscv32")
	if err != nil {
		t.Skip("qemu-riscv32 not installed")
	}
	cm, err := CompileModule(riscv32MixedTypedIfModule(t))
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
	if !a.PatchJAL21(call, len(a.B)+cm.Entry[0]) {
		t.Fatal("wrapper call relocation")
	}
	runRV32Exit(t, qemu, append(a.B, cm.Code...), 42)
}

func riscv32MixedMultiValueIfModule(t *testing.T) *wasm.Module {
	t.Helper()
	body := []byte{1, 1, 0x7d,
		0x41, 0, 0x04, 0,
		0x42, 37, 0x41, 1,
		0x05, 0x42, 42, 0x41, 2,
		0x0b, 0x0b}
	code := append(wasmtest.ULEB(uint32(len(body))), body...)
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I64, wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(10, wasmtest.Vec(code)),
	))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCompileModuleMergesTypeIndexedMultiValueIfUnderQEMU(t *testing.T) {
	qemu, err := exec.LookPath("qemu-riscv32")
	if err != nil {
		t.Skip("qemu-riscv32 not installed")
	}
	cm, err := CompileModule(riscv32MixedMultiValueIfModule(t))
	if err != nil {
		t.Fatal(err)
	}
	var a rv.Asm
	rvMemoryContext(&a)
	a.Addi(rv.A0, rv.SP, 16)
	a.MovReg(rv.X23, rv.A0)
	call := a.Jal(rv.RA)
	a.Add(rv.A0, rv.A0, rv.A2)
	a.MovImm32(rv.A7, 93)
	a.Ecall()
	if !a.PatchJAL21(call, len(a.B)+cm.Entry[0]) {
		t.Fatal("wrapper call relocation")
	}
	runRV32Exit(t, qemu, append(a.B, cm.Code...), 44)
}

func riscv32MixedParameterizedIfModule(t *testing.T) *wasm.Module {
	t.Helper()
	body := []byte{1, 1, 0x7d,
		0x42, 40, 0x41, 0, 0x04, 0,
		0x42, 1, 0x7c,
		0x05, 0x42, 2, 0x7c,
		0x0b, 0x0b}
	code := append(wasmtest.ULEB(uint32(len(body))), body...)
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I64}),
		)),
		wasmtest.Section(3, wasmtest.Vec([]byte{1})),
		wasmtest.Section(10, wasmtest.Vec(code)),
	))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCompileModuleMergesParameterizedWideIfUnderQEMU(t *testing.T) {
	qemu, err := exec.LookPath("qemu-riscv32")
	if err != nil {
		t.Skip("qemu-riscv32 not installed")
	}
	cm, err := CompileModule(riscv32MixedParameterizedIfModule(t))
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
	if !a.PatchJAL21(call, len(a.B)+cm.Entry[0]) {
		t.Fatal("wrapper call relocation")
	}
	runRV32Exit(t, qemu, append(a.B, cm.Code...), 42)
}

func riscv32MixedTypedLoopModule(t *testing.T) *wasm.Module {
	t.Helper()
	body := []byte{2, 1, 0x7f, 1, 0x7e,
		0x41, 2, 0x42, 40, 0x03, 0,
		0x21, 1, 0x21, 0,
		0x20, 1, 0x42, 1, 0x7c, 0x21, 1,
		0x20, 0, 0x41, 1, 0x6b, 0x21, 0,
		0x20, 0, 0x20, 1, 0x20, 0, 0x0d, 0,
		0x21, 1, 0x1a, 0x20, 1,
		0x0b, 0x0b}
	code := append(wasmtest.ULEB(uint32(len(body))), body...)
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I64}, []wasm.ValType{wasm.I64}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I64}),
		)),
		wasmtest.Section(3, wasmtest.Vec([]byte{1})),
		wasmtest.Section(10, wasmtest.Vec(code)),
	))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCompileModuleCarriesTypedWideLoopParametersUnderQEMU(t *testing.T) {
	qemu, err := exec.LookPath("qemu-riscv32")
	if err != nil {
		t.Skip("qemu-riscv32 not installed")
	}
	cm, err := CompileModule(riscv32MixedTypedLoopModule(t))
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
	if !a.PatchJAL21(call, len(a.B)+cm.Entry[0]) {
		t.Fatal("wrapper call relocation")
	}
	runRV32Exit(t, qemu, append(a.B, cm.Code...), 42)
}

func riscv32MixedBranchModule(t *testing.T) *wasm.Module {
	t.Helper()
	body := []byte{1, 1, 0x7d,
		0x02, 0x7e, 0x42, 42, 0x0c, 0, 0x0b, 0x0b}
	code := append(wasmtest.ULEB(uint32(len(body))), body...)
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(10, wasmtest.Vec(code)),
	))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCompileModuleBranchesWithWideResultUnderQEMU(t *testing.T) {
	qemu, err := exec.LookPath("qemu-riscv32")
	if err != nil {
		t.Skip("qemu-riscv32 not installed")
	}
	cm, err := CompileModule(riscv32MixedBranchModule(t))
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
	if !a.PatchJAL21(call, len(a.B)+cm.Entry[0]) {
		t.Fatal("wrapper call relocation")
	}
	runRV32Exit(t, qemu, append(a.B, cm.Code...), 42)
}

func riscv32MixedDeepBranchModule(t *testing.T) *wasm.Module {
	t.Helper()
	outer := wasmtest.Code([]byte{
		0x02, 0x7e,
		0x02, 0x40,
		0x42, 42,
		0x0c, 1,
		0x41, 1, 0x1a,
		0x02, 0x40, 0x41, 2, 0x1a, 0x0b,
		0x0b,
		0x0b,
		0x0b,
	})
	conditional := wasmtest.Code([]byte{
		0x02, 0x7e,
		0x41, 7,
		0x42, 42,
		0x41, 1,
		0x0d, 0,
		0x1a,
		0x1a,
		0x42, 0,
		0x0b,
		0x0b,
	})
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{0})),
		wasmtest.Section(10, wasmtest.Vec(outer, conditional)),
	))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCompileModuleBranchesAcrossNestedWideControlsUnderQEMU(t *testing.T) {
	qemu, err := exec.LookPath("qemu-riscv32")
	if err != nil {
		t.Skip("qemu-riscv32 not installed")
	}
	cm, err := CompileModule(riscv32MixedDeepBranchModule(t))
	if err != nil {
		t.Fatal(err)
	}
	var a rv.Asm
	rvMemoryContext(&a)
	a.Addi(rv.A0, rv.SP, 16)
	a.MovReg(rv.X23, rv.A0)
	first := a.Jal(rv.RA)
	a.MovReg(rv.S0, rv.A0)
	second := a.Jal(rv.RA)
	a.Add(rv.A0, rv.A0, rv.S0)
	a.MovImm32(rv.A7, 93)
	a.Ecall()
	if !a.PatchJAL21(first, len(a.B)+cm.Entry[0]) || !a.PatchJAL21(second, len(a.B)+cm.Entry[1]) {
		t.Fatal("wrapper call relocation")
	}
	runRV32Exit(t, qemu, append(a.B, cm.Code...), 84)
}

func riscv32MixedBranchTableModule(t *testing.T) *wasm.Module {
	t.Helper()
	body := []byte{
		0x02, 0x7e,
		0x02, 0x7e,
		0x42, 40,
		0x20, 0,
		0x0e, 2, 0, 1, 1,
		0x0b,
		0x42, 2, 0x7c,
		0x0b,
		0x0b,
	}
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCompileModuleBranchesThroughWideTableTargetsUnderQEMU(t *testing.T) {
	qemu, err := exec.LookPath("qemu-riscv32")
	if err != nil {
		t.Skip("qemu-riscv32 not installed")
	}
	cm, err := CompileModule(riscv32MixedBranchTableModule(t))
	if err != nil {
		t.Fatal(err)
	}
	var a rv.Asm
	rvMemoryContext(&a)
	a.Addi(rv.A0, rv.SP, 16)
	a.MovReg(rv.X23, rv.A0)
	a.MovImm32(rv.A0, 0)
	first := a.Jal(rv.RA)
	a.MovReg(rv.S0, rv.A0)
	a.MovImm32(rv.A0, 1)
	second := a.Jal(rv.RA)
	a.Add(rv.A0, rv.A0, rv.S0)
	a.MovImm32(rv.A7, 93)
	a.Ecall()
	if !a.PatchJAL21(first, len(a.B)+cm.Entry[0]) || !a.PatchJAL21(second, len(a.B)+cm.Entry[0]) {
		t.Fatal("wrapper call relocation")
	}
	runRV32Exit(t, qemu, append(a.B, cm.Code...), 82)
}

func riscv32MixedSIMDHelperModule(t *testing.T) *wasm.Module {
	t.Helper()
	first := make([]byte, 16)
	first[0] = 20
	second := make([]byte, 16)
	second[0] = 22
	body := []byte{1, 1, 0x7f, 0xfd, 12}
	body = append(body, first...)
	body = append(body, 0xfd, 12)
	body = append(body, second...)
	body = append(body, 0xfd, 0xe4, 0x01, 0x0b)
	code := append(wasmtest.ULEB(uint32(len(body))), body...)
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.V128}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(10, wasmtest.Vec(code)),
	))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCompileModuleDispatchesSIMDHelperFromMixedFunctionUnderQEMU(t *testing.T) {
	qemu, err := exec.LookPath("qemu-riscv32")
	if err != nil {
		t.Skip("qemu-riscv32 not installed")
	}
	cm, err := CompileModule(riscv32MixedSIMDHelperModule(t))
	if err != nil {
		t.Fatal(err)
	}
	var helper rv.Asm
	helper.Lw(rv.T0, rv.A0, embedded32.SIMDFrameAOffset)
	helper.Lw(rv.T1, rv.A0, embedded32.SIMDFrameBOffset)
	helper.Add(rv.T0, rv.T0, rv.T1)
	helper.Sw(rv.T0, rv.A0, embedded32.SIMDFrameOutOffset)
	for i := int32(1); i < 4; i++ {
		helper.Sw(rv.Zero, rv.A0, embedded32.SIMDFrameOutOffset+i*4)
	}
	helper.Sw(rv.Zero, rv.A0, embedded32.SIMDFrameTrapOffset)
	helper.Ret()
	buildWrapper := func(table uint32) rv.Asm {
		var a rv.Asm
		rvMemoryContext(&a)
		a.Addi(rv.T0, rv.SP, 84)
		a.Sw(rv.T0, rv.SP, 24)
		a.Sw(rv.Zero, rv.SP, 84)
		a.MovImm32(rv.T0, table)
		a.Sw(rv.T0, rv.SP, 32)
		a.Addi(rv.A0, rv.SP, 16)
		a.MovReg(rv.X23, rv.A0)
		call := a.Jal(rv.RA)
		a.MovImm32(rv.A7, 93)
		a.Ecall()
		if !a.PatchJAL21(call, len(a.B)+cm.Entry[0]) {
			t.Fatal("wrapper call relocation")
		}
		return a
	}
	const base = uint32(0x10000)
	wrapper := buildWrapper(base)
	for {
		helperOff := len(wrapper.B) + len(cm.Code)
		tableOff := helperOff + len(helper.B)
		next := buildWrapper(base + uint32(tableOff))
		if len(next.B) == len(wrapper.B) {
			wrapper = next
			break
		}
		wrapper = next
	}
	helperOff := len(wrapper.B) + len(cm.Code)
	image := append(wrapper.B, cm.Code...)
	image = append(image, helper.B...)
	var table [embedded32.HelperTableBytes]byte
	binary.LittleEndian.PutUint32(table[embedded32.HelperSIMDOffset:], base+uint32(helperOff))
	image = append(image, table[:]...)
	runRV32Exit(t, qemu, image, 42)
}

func riscv32MixedSIMDMemoryModule(t *testing.T) *wasm.Module {
	t.Helper()
	body := []byte{1, 1, 0x7f, 0x41, 0, 0xfd, 0, 4, 0, 0x0b}
	code := append(wasmtest.ULEB(uint32(len(body))), body...)
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.V128}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})),
		wasmtest.Section(10, wasmtest.Vec(code)),
	))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCompileModuleDispatchesSIMDMemoryHelperUnderQEMU(t *testing.T) {
	qemu, err := exec.LookPath("qemu-riscv32")
	if err != nil {
		t.Skip("qemu-riscv32 not installed")
	}
	cm, err := CompileModule(riscv32MixedSIMDMemoryModule(t))
	if err != nil {
		t.Fatal(err)
	}
	var helper rv.Asm
	helper.Lw(rv.T0, rv.A0, embedded32.SIMDFrameMemoryBaseOffset)
	helper.Lw(rv.T1, rv.A0, embedded32.SIMDFrameAddressOffset)
	helper.Add(rv.T0, rv.T0, rv.T1)
	helper.Lw(rv.T1, rv.T0, 0)
	helper.Sw(rv.T1, rv.A0, embedded32.SIMDFrameOutOffset)
	for i := int32(1); i < 4; i++ {
		helper.Sw(rv.Zero, rv.A0, embedded32.SIMDFrameOutOffset+i*4)
	}
	helper.Sw(rv.Zero, rv.A0, embedded32.SIMDFrameTrapOffset)
	helper.Ret()
	buildWrapper := func(table uint32) rv.Asm {
		var a rv.Asm
		rvMemoryContext(&a)
		a.MovImm32(rv.T0, 42)
		a.Sw(rv.T0, rv.SP, 0)
		a.Addi(rv.T0, rv.SP, 84)
		a.Sw(rv.T0, rv.SP, 24)
		a.Sw(rv.Zero, rv.SP, 84)
		a.MovImm32(rv.T0, table)
		a.Sw(rv.T0, rv.SP, 32)
		a.Addi(rv.A0, rv.SP, 16)
		a.MovReg(rv.X23, rv.A0)
		call := a.Jal(rv.RA)
		a.MovImm32(rv.A7, 93)
		a.Ecall()
		if !a.PatchJAL21(call, len(a.B)+cm.Entry[0]) {
			t.Fatal("wrapper call relocation")
		}
		return a
	}
	const base = uint32(0x10000)
	wrapper := buildWrapper(base)
	for {
		helperOff := len(wrapper.B) + len(cm.Code)
		tableOff := helperOff + len(helper.B)
		next := buildWrapper(base + uint32(tableOff))
		if len(next.B) == len(wrapper.B) {
			wrapper = next
			break
		}
		wrapper = next
	}
	helperOff := len(wrapper.B) + len(cm.Code)
	image := append(wrapper.B, cm.Code...)
	image = append(image, helper.B...)
	var table [embedded32.HelperTableBytes]byte
	binary.LittleEndian.PutUint32(table[embedded32.HelperSIMDOffset:], base+uint32(helperOff))
	image = append(image, table[:]...)
	runRV32Exit(t, qemu, image, 42)
}

func riscv32MixedF32HelperModule(t *testing.T) *wasm.Module {
	t.Helper()
	body := []byte{1, 1, 0x7e,
		0x43, 20, 0, 0, 0,
		0x43, 22, 0, 0, 0,
		0x92, 0x0b}
	code := append(wasmtest.ULEB(uint32(len(body))), body...)
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.F32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(10, wasmtest.Vec(code)),
	))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCompileModuleDispatchesF32HelperFromMixedFunctionUnderQEMU(t *testing.T) {
	qemu, err := exec.LookPath("qemu-riscv32")
	if err != nil {
		t.Skip("qemu-riscv32 not installed")
	}
	cm, err := CompileModule(riscv32MixedF32HelperModule(t))
	if err != nil {
		t.Fatal(err)
	}
	var helper rv.Asm
	helper.Lw(rv.T0, rv.A0, embedded32.F32FrameALoOffset)
	helper.Lw(rv.T1, rv.A0, embedded32.F32FrameBLoOffset)
	helper.Add(rv.T0, rv.T0, rv.T1)
	helper.Sw(rv.T0, rv.A0, embedded32.F32FrameOutLoOffset)
	helper.Sw(rv.Zero, rv.A0, embedded32.F32FrameOutHiOffset)
	helper.Sw(rv.Zero, rv.A0, embedded32.F32FrameTrapOffset)
	helper.Ret()
	buildWrapper := func(table uint32) rv.Asm {
		var a rv.Asm
		rvMemoryContext(&a)
		a.Addi(rv.T0, rv.SP, 84)
		a.Sw(rv.T0, rv.SP, 24)
		a.Sw(rv.Zero, rv.SP, 84)
		a.MovImm32(rv.T0, table)
		a.Sw(rv.T0, rv.SP, 32)
		a.Addi(rv.A0, rv.SP, 16)
		a.MovReg(rv.X23, rv.A0)
		call := a.Jal(rv.RA)
		a.MovImm32(rv.A7, 93)
		a.Ecall()
		if !a.PatchJAL21(call, len(a.B)+cm.Entry[0]) {
			t.Fatal("wrapper call relocation")
		}
		return a
	}
	const base = uint32(0x10000)
	wrapper := buildWrapper(base)
	for {
		helperOff := len(wrapper.B) + len(cm.Code)
		tableOff := helperOff + len(helper.B)
		next := buildWrapper(base + uint32(tableOff))
		if len(next.B) == len(wrapper.B) {
			wrapper = next
			break
		}
		wrapper = next
	}
	helperOff := len(wrapper.B) + len(cm.Code)
	image := append(wrapper.B, cm.Code...)
	image = append(image, helper.B...)
	var table [embedded32.HelperTableBytes]byte
	binary.LittleEndian.PutUint32(table[embedded32.HelperF32Offset:], base+uint32(helperOff))
	image = append(image, table[:]...)
	runRV32Exit(t, qemu, image, 42)
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

func riscv32MemoryInitModule(t *testing.T) *wasm.Module {
	t.Helper()
	body := []byte{0x41, 0, 0x41, 0, 0x41, 3, 0xfc, 8, 0, 0, 0xfc, 9, 0, 0x41, 0, 0x2d, 0, 0, 0x0b}
	passive := append([]byte{1}, wasmtest.ULEB(3)...)
	passive = append(passive, 'x', 'y', 'z')
	count := uint32(1)
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})),
		wasmtest.Section(12, wasmtest.ULEB(count)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
		wasmtest.Section(11, wasmtest.Vec(passive)),
	))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCompileModuleMemoryInitUnderQEMU(t *testing.T) {
	qemu, err := exec.LookPath("qemu-riscv32")
	if err != nil {
		t.Skip("qemu-riscv32 not installed")
	}
	cm, err := CompileModule(riscv32MemoryInitModule(t))
	if err != nil {
		t.Fatal(err)
	}
	meta := cm.Functions[0]
	fn := cm.Code[meta.Offset : meta.Offset+meta.Size]
	var a rv.Asm
	rvMemoryContext(&a)
	a.Addi(rv.T1, rv.SP, 80)
	a.Sw(rv.T1, rv.SP, 64)
	a.MovImm32(rv.T1, 3)
	a.Sw(rv.T1, rv.SP, 68)
	a.MovImm32(rv.T1, 0)
	a.Sw(rv.T1, rv.SP, 72)
	a.MovImm32(rv.T1, 0x007a7978)
	a.Sw(rv.T1, rv.SP, 80)
	a.MovImm32(rv.T1, 1)
	a.Sw(rv.T1, rv.SP, 52)
	a.Addi(rv.A0, rv.SP, 16)
	a.MovReg(rv.X23, rv.A0)
	call := a.Jal(rv.RA)
	a.MovImm32(rv.A7, 93)
	a.Ecall()
	a.PatchJAL21(call, len(a.B))
	runRV32Exit(t, qemu, append(a.B, fn...), 120)
}

func riscv32BulkModule(t *testing.T) *wasm.Module {
	t.Helper()
	body := []byte{
		0x41, 0, 0x41, 42, 0x41, 4, 0xfc, 11, 0,
		0x41, 4, 0x41, 0, 0x41, 4, 0xfc, 10, 0, 0,
		0x41, 7, 0x2d, 0, 0, 0x0b,
	}
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCompileModuleBulkMemoryUnderQEMU(t *testing.T) {
	qemu, err := exec.LookPath("qemu-riscv32")
	if err != nil {
		t.Skip("qemu-riscv32 not installed")
	}
	cm, err := CompileModule(riscv32BulkModule(t))
	if err != nil {
		t.Fatal(err)
	}
	meta := cm.Functions[0]
	fn := cm.Code[meta.Offset : meta.Offset+meta.Size]
	var a rv.Asm
	rvMemoryContext(&a)
	a.Addi(rv.A0, rv.SP, 16)
	a.MovReg(rv.X23, rv.A0)
	call := a.Jal(rv.RA)
	a.MovImm32(rv.A7, 93)
	a.Ecall()
	a.PatchJAL21(call, len(a.B))
	runRV32Exit(t, qemu, append(a.B, fn...), 42)
}

func riscv32MixedMemoryInitModule(t *testing.T) *wasm.Module {
	t.Helper()
	body := []byte{1, 1, 0x7c, 0x41, 0, 0x41, 0, 0x41, 3, 0xfc, 8, 0, 0, 0xfc, 9, 0, 0x41, 0, 0x2d, 0, 0, 0x0b}
	passive := append([]byte{1}, wasmtest.ULEB(3)...)
	passive = append(passive, 'x', 'y', 'z')
	code := append(wasmtest.ULEB(uint32(len(body))), body...)
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})),
		wasmtest.Section(12, wasmtest.ULEB(1)),
		wasmtest.Section(10, wasmtest.Vec(code)),
		wasmtest.Section(11, wasmtest.Vec(passive)),
	))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCompileModuleMemoryInitFromMixedFunctionUnderQEMU(t *testing.T) {
	qemu, err := exec.LookPath("qemu-riscv32")
	if err != nil {
		t.Skip("qemu-riscv32 not installed")
	}
	cm, err := CompileModule(riscv32MixedMemoryInitModule(t))
	if err != nil {
		t.Fatal(err)
	}
	meta := cm.Functions[0]
	fn := cm.Code[meta.Offset : meta.Offset+meta.Size]
	var a rv.Asm
	rvMemoryContext(&a)
	a.Addi(rv.T0, rv.SP, 80)
	a.Sw(rv.T0, rv.SP, 64)
	a.MovImm32(rv.T0, 3)
	a.Sw(rv.T0, rv.SP, 68)
	a.Sw(rv.Zero, rv.SP, 72)
	a.MovImm32(rv.T0, 0x007a7978)
	a.Sw(rv.T0, rv.SP, 80)
	a.MovImm32(rv.T0, 1)
	a.Sw(rv.T0, rv.SP, 52)
	a.Addi(rv.A0, rv.SP, 16)
	a.MovReg(rv.X23, rv.A0)
	call := a.Jal(rv.RA)
	a.MovImm32(rv.A7, 93)
	a.Ecall()
	a.PatchJAL21(call, len(a.B))
	runRV32Exit(t, qemu, append(a.B, fn...), 120)
}

func riscv32MixedBulkModule(t *testing.T) *wasm.Module {
	t.Helper()
	body := []byte{1, 1, 0x7c,
		0x41, 0, 0x41, 42, 0x41, 4, 0xfc, 11, 0,
		0x41, 4, 0x41, 0, 0x41, 4, 0xfc, 10, 0, 0,
		0x41, 7, 0x2d, 0, 0, 0x0b,
	}
	code := append(wasmtest.ULEB(uint32(len(body))), body...)
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})),
		wasmtest.Section(10, wasmtest.Vec(code)),
	))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCompileModuleBulkMemoryFromMixedFunctionUnderQEMU(t *testing.T) {
	qemu, err := exec.LookPath("qemu-riscv32")
	if err != nil {
		t.Skip("qemu-riscv32 not installed")
	}
	cm, err := CompileModule(riscv32MixedBulkModule(t))
	if err != nil {
		t.Fatal(err)
	}
	meta := cm.Functions[0]
	fn := cm.Code[meta.Offset : meta.Offset+meta.Size]
	var a rv.Asm
	rvMemoryContext(&a)
	a.Addi(rv.A0, rv.SP, 16)
	a.MovReg(rv.X23, rv.A0)
	call := a.Jal(rv.RA)
	a.MovImm32(rv.A7, 93)
	a.Ecall()
	a.PatchJAL21(call, len(a.B))
	runRV32Exit(t, qemu, append(a.B, fn...), 42)
}

func riscv32ImportedGlobalModule(t *testing.T) *wasm.Module {
	t.Helper()
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I64}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(
			wasmtest.GlobalImportEntry("env", "base", wasm.I64, false),
			wasmtest.GlobalImportEntry("env", "wide", wasm.I64, true),
			wasmtest.GlobalImportEntry("env", "word", wasm.I32, true),
		)),
		wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{1})),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I64, false, []byte{0x23, 0, 0x0b}))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x23, 1, 0x42, 2, 0x7c, 0x24, 1, 0x23, 3, 0x23, 1, 0x7c, 0x0b}),
			wasmtest.Code([]byte{0x23, 2, 0x41, 1, 0x6a, 0x24, 2, 0x23, 2, 0x0b}),
		)),
	))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCompileModuleAccessesImportedGlobalsUnderQEMU(t *testing.T) {
	qemu, err := exec.LookPath("qemu-riscv32")
	if err != nil {
		t.Skip("qemu-riscv32 not installed")
	}
	cm, err := CompileModule(riscv32ImportedGlobalModule(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(cm.ImportedGlobals) != 3 || len(cm.Globals) != 1 || !cm.Globals[0].HasInitGlobal || cm.Globals[0].InitGlobal != 0 {
		t.Fatalf("imported=%+v globals=%+v", cm.ImportedGlobals, cm.Globals)
	}
	localCells := make([]uint32, 2)
	if err := cm.InstantiateGlobalsWithImports(localCells, [][]uint32{{10, 0}, {30, 0}, {40}}); err != nil || localCells[0] != 10 || localCells[1] != 0 {
		t.Fatalf("local globals=%v err=%v", localCells, err)
	}
	var a rv.Asm
	rvMemoryContext(&a)
	a.Addi(rv.T0, rv.SP, 104)
	a.Sw(rv.T0, rv.SP, 44)
	a.Addi(rv.T0, rv.SP, 72)
	a.Sw(rv.T0, rv.SP, 64)
	a.Addi(rv.T0, rv.SP, 84)
	a.Sw(rv.T0, rv.SP, 72)
	a.Addi(rv.T0, rv.SP, 92)
	a.Sw(rv.T0, rv.SP, 76)
	a.Addi(rv.T0, rv.SP, 100)
	a.Sw(rv.T0, rv.SP, 80)
	a.MovImm32(rv.T0, 10)
	a.Sw(rv.T0, rv.SP, 84)
	a.Sw(rv.Zero, rv.SP, 88)
	a.MovImm32(rv.T0, 30)
	a.Sw(rv.T0, rv.SP, 92)
	a.Sw(rv.Zero, rv.SP, 96)
	a.MovImm32(rv.T0, 40)
	a.Sw(rv.T0, rv.SP, 100)
	a.MovImm32(rv.T0, localCells[0])
	a.Sw(rv.T0, rv.SP, 104)
	a.MovImm32(rv.T0, localCells[1])
	a.Sw(rv.T0, rv.SP, 108)
	a.Addi(rv.A0, rv.SP, 16)
	a.MovReg(rv.X23, rv.A0)
	first := a.Jal(rv.RA)
	a.MovReg(rv.S0, rv.A0)
	second := a.Jal(rv.RA)
	a.Add(rv.A0, rv.A0, rv.S0)
	a.MovImm32(rv.A7, 93)
	a.Ecall()
	if !a.PatchJAL21(first, len(a.B)+cm.Entry[0]) || !a.PatchJAL21(second, len(a.B)+cm.Entry[1]) {
		t.Fatal("wrapper call relocation")
	}
	runRV32Exit(t, qemu, append(a.B, cm.Code...), 83)
}

func riscv32ImportedMemoryModule(t *testing.T) *wasm.Module {
	t.Helper()
	memoryImport := append(wasmtest.Name("env"), wasmtest.Name("memory")...)
	memoryImport = append(memoryImport, 2, 0, 0)
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(memoryImport)),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x2d, 0, 0, 0x0b}))),
	))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCompileModuleAccessesImportedMemoryUnderQEMU(t *testing.T) {
	qemu, err := exec.LookPath("qemu-riscv32")
	if err != nil {
		t.Skip("qemu-riscv32 not installed")
	}
	cm, err := CompileModule(riscv32ImportedMemoryModule(t))
	if err != nil {
		t.Fatal(err)
	}
	if !cm.MemoryImported {
		t.Fatal("memory import metadata not retained")
	}
	var a rv.Asm
	rvMemoryContext(&a)
	a.MovImm32(rv.T0, 7)
	a.Sb(rv.T0, rv.SP, 0)
	a.MovImm32(rv.T0, 42)
	a.Sb(rv.T0, rv.SP, 4)
	a.Addi(rv.T0, rv.SP, 4)
	a.Sw(rv.T0, rv.SP, 72)
	a.MovImm32(rv.T0, 16)
	a.Sw(rv.T0, rv.SP, 76)
	a.Sw(rv.T0, rv.SP, 92)
	a.Addi(rv.T0, rv.SP, 72)
	a.Sw(rv.T0, rv.SP, 68)
	a.Addi(rv.A0, rv.SP, 16)
	a.MovReg(rv.X23, rv.A0)
	a.MovImm32(rv.A0, 0)
	call := a.Jal(rv.RA)
	a.MovImm32(rv.A7, 93)
	a.Ecall()
	if !a.PatchJAL21(call, len(a.B)+cm.Entry[0]) {
		t.Fatal("wrapper call relocation")
	}
	runRV32Exit(t, qemu, append(a.B, cm.Code...), 42)
}

func riscv32ImportedTableModule(t *testing.T) *wasm.Module {
	t.Helper()
	tableImport := append(wasmtest.Name("env"), wasmtest.Name("table")...)
	tableImport = append(tableImport, 1, 0x70, 1, 1, 1)
	body := []byte{1, 1, 0x7c, 0x20, 0, 0x25, 0, 0xd1, 0x41, 42, 0x6a, 0x0b}
	code := append(wasmtest.ULEB(uint32(len(body))), body...)
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(tableImport)),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(10, wasmtest.Vec(code)),
	))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCompileModuleAccessesImportedTableUnderQEMU(t *testing.T) {
	qemu, err := exec.LookPath("qemu-riscv32")
	if err != nil {
		t.Skip("qemu-riscv32 not installed")
	}
	cm, err := CompileModule(riscv32ImportedTableModule(t))
	if err != nil {
		t.Fatal(err)
	}
	entries := []uint32{7}
	if cm.Table == nil || !cm.Table.Imported {
		t.Fatalf("table metadata=%+v", cm.Table)
	}
	if err := cm.InstantiateTable(entries); err != nil || entries[0] != 7 {
		t.Fatalf("imported table entries=%v err=%v", entries, err)
	}
	var a rv.Asm
	rvMemoryContext(&a)
	a.Addi(rv.T0, rv.SP, 124)
	a.Sw(rv.T0, rv.SP, 28)
	a.Addi(rv.T0, rv.SP, 76)
	a.Sw(rv.T0, rv.SP, 56)
	a.Sw(rv.T0, rv.SP, 72)
	a.Addi(rv.T0, rv.SP, 104)
	a.Sw(rv.T0, rv.SP, 76)
	a.MovImm32(rv.T0, 1)
	a.Sw(rv.T0, rv.SP, 80)
	a.Sw(rv.T0, rv.SP, 84)
	a.Sw(rv.Zero, rv.SP, 88)
	a.Sw(rv.Zero, rv.SP, 92)
	a.Sw(rv.Zero, rv.SP, 96)
	a.Sw(rv.Zero, rv.SP, 100)
	a.MovImm32(rv.T0, entries[0])
	a.Sw(rv.T0, rv.SP, 104)
	a.Sw(rv.Zero, rv.SP, 124)
	a.Addi(rv.A0, rv.SP, 16)
	a.MovReg(rv.X23, rv.A0)
	a.MovImm32(rv.A0, 0)
	call := a.Jal(rv.RA)
	a.MovImm32(rv.A7, 93)
	a.Ecall()
	if !a.PatchJAL21(call, len(a.B)+cm.Entry[0]) {
		t.Fatal("wrapper call relocation")
	}
	runRV32Exit(t, qemu, append(a.B, cm.Code...), 42)
}

func riscv32MixedToI32CallModule(t *testing.T) *wasm.Module {
	t.Helper()
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{1})),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0x41, 1, 0x6a, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0xa7, 0x10, 0, 0x0b}),
		)),
	))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCompileModuleCallsI32FunctionFromMixedFrameUnderQEMU(t *testing.T) {
	qemu, err := exec.LookPath("qemu-riscv32")
	if err != nil {
		t.Skip("qemu-riscv32 not installed")
	}
	cm, err := CompileModule(riscv32MixedToI32CallModule(t))
	if err != nil {
		t.Fatal(err)
	}
	var a rv.Asm
	rvMemoryContext(&a)
	a.Addi(rv.A0, rv.SP, 16)
	a.MovReg(rv.X23, rv.A0)
	a.MovImm32(rv.A0, 41)
	a.MovImm32(rv.A1, 0)
	call := a.Jal(rv.RA)
	a.MovImm32(rv.A7, 93)
	a.Ecall()
	if !a.PatchJAL21(call, len(a.B)+cm.Entry[1]) {
		t.Fatal("wrapper call relocation")
	}
	runRV32Exit(t, qemu, append(a.B, cm.Code...), 42)
}

func riscv32ImportedCallModule(t *testing.T) *wasm.Module {
	t.Helper()
	importI32 := append(wasmtest.Name("env"), wasmtest.Name("i32")...)
	importI32 = append(importI32, 0, 0)
	importI64 := append(wasmtest.Name("env"), wasmtest.Name("i64")...)
	importI64 = append(importI64, 0, 1)
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}),
		)),
		wasmtest.Section(2, wasmtest.Vec(importI32, importI64)),
		wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{1})),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0x10, 0, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x10, 1, 0x0b}),
		)),
	))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCompileModuleCallsImportedCallbacksUnderQEMU(t *testing.T) {
	qemu, err := exec.LookPath("qemu-riscv32")
	if err != nil {
		t.Skip("qemu-riscv32 not installed")
	}
	cm, err := CompileModule(riscv32ImportedCallModule(t))
	if err != nil {
		t.Fatal(err)
	}
	if cm.ImportedFunctions != 2 || len(cm.FunctionTypeIDs) != 4 || cm.Functions[0].FuncIndex != 2 || cm.Functions[1].FuncIndex != 3 {
		t.Fatalf("imports=%d typeIDs=%v functions=%+v", cm.ImportedFunctions, cm.FunctionTypeIDs, cm.Functions)
	}
	var callback rv.Asm
	callback.Addi(rv.A0, rv.A0, 1)
	callback.Ret()
	const base = uint32(0x10000)
	buildWrapper := func(callbackAddress uint32) rv.Asm {
		var a rv.Asm
		rvMemoryContext(&a)
		a.Addi(rv.T0, rv.SP, 96)
		a.Sw(rv.T0, rv.SP, 60)
		a.MovImm32(rv.T0, callbackAddress)
		a.Sw(rv.T0, rv.SP, 96)
		a.Sw(rv.T0, rv.SP, 104)
		a.Addi(rv.A0, rv.SP, 16)
		a.Sw(rv.A0, rv.SP, 100)
		a.Sw(rv.A0, rv.SP, 108)
		a.MovReg(rv.X23, rv.A0)
		a.MovImm32(rv.A0, 41)
		first := a.Jal(rv.RA)
		a.MovReg(rv.S0, rv.A0)
		a.MovImm32(rv.A0, 40)
		a.MovImm32(rv.A1, 0)
		second := a.Jal(rv.RA)
		a.Add(rv.A0, rv.A0, rv.S0)
		a.MovImm32(rv.A7, 93)
		a.Ecall()
		if !a.PatchJAL21(first, len(a.B)+cm.Entry[0]) || !a.PatchJAL21(second, len(a.B)+cm.Entry[1]) {
			t.Fatal("wrapper call relocation")
		}
		return a
	}
	wrapper := buildWrapper(base)
	for {
		callbackAddress := base + uint32(len(wrapper.B)+len(cm.Code))
		next := buildWrapper(callbackAddress)
		if len(next.B) == len(wrapper.B) {
			wrapper = next
			break
		}
		wrapper = next
	}
	image := append(wrapper.B, cm.Code...)
	image = append(image, callback.B...)
	runRV32Exit(t, qemu, image, 83)
}

func riscv32LinkedImportModule(t *testing.T) *wasm.Module {
	t.Helper()
	importFunction := append(wasmtest.Name("env"), wasmtest.Name("f")...)
	importFunction = append(importFunction, 0, 0)
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(importFunction)),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, false, []byte{0x41, 1, 0x0b}))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0, 0x23, 0, 0x6a, 0x0b}))),
	))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func riscv32LinkedProviderModule(t *testing.T, trapping bool) *wasm.Module {
	t.Helper()
	body := []byte{0x23, 0, 0x0b}
	if trapping {
		body = []byte{0x00, 0x0b}
	}
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, false, []byte{0x41, 42, 0x0b}))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCompileModuleSwitchesLinkedImportContextUnderQEMU(t *testing.T) {
	qemu, err := exec.LookPath("qemu-riscv32")
	if err != nil {
		t.Skip("qemu-riscv32 not installed")
	}
	cm, err := CompileModule(riscv32LinkedImportModule(t))
	if err != nil {
		t.Fatal(err)
	}
	build := func(callbackAddress uint32, trapping bool) (a rv.Asm) {
		rvMemoryContext(&a)
		a.Addi(rv.SP, rv.SP, -128)
		a.Addi(rv.T0, rv.SP, 80)
		a.Sw(rv.T0, rv.SP, 172)
		a.Addi(rv.T0, rv.SP, 88)
		a.Sw(rv.T0, rv.SP, 188)
		a.MovImm32(rv.T0, callbackAddress)
		a.Sw(rv.T0, rv.SP, 88)
		a.Addi(rv.T0, rv.SP, 16)
		a.Sw(rv.T0, rv.SP, 92)
		a.MovImm32(rv.T0, 1)
		a.Sw(rv.T0, rv.SP, 80)
		a.Addi(rv.T0, rv.SP, 68)
		a.Sw(rv.T0, rv.SP, 24)
		a.Addi(rv.T0, rv.SP, 72)
		a.Sw(rv.T0, rv.SP, 28)
		a.Addi(rv.T0, rv.SP, 76)
		a.Sw(rv.T0, rv.SP, 44)
		a.Sw(rv.Zero, rv.SP, 68)
		a.Sw(rv.Zero, rv.SP, 72)
		a.MovImm32(rv.T0, 42)
		a.Sw(rv.T0, rv.SP, 76)
		a.Addi(rv.A0, rv.SP, 144)
		a.MovReg(rv.X23, rv.A0)
		call := a.Jal(rv.RA)
		if trapping {
			a.Lw(rv.A0, rv.SP, 160)
		}
		a.MovImm32(rv.A7, 93)
		a.Ecall()
		if !a.PatchJAL21(call, len(a.B)+cm.Entry[0]) {
			t.Fatal("wrapper call relocation")
		}
		return a
	}
	for _, trapping := range []bool{false, true} {
		t.Run(map[bool]string{false: "restore", true: "trap"}[trapping], func(t *testing.T) {
			provider, err := CompileModule(riscv32LinkedProviderModule(t, trapping))
			if err != nil {
				t.Fatal(err)
			}
			const base = uint32(0x10000)
			wrapper := build(base, trapping)
			providerAddress := base + uint32(len(wrapper.B)+len(cm.Code)+provider.Entry[0])
			wrapper = build(providerAddress, trapping)
			image := append(wrapper.B, cm.Code...)
			image = append(image, provider.Code...)
			want := 43
			if trapping {
				want = int(embedded32.TrapUnreachable)
			}
			runRV32Exit(t, qemu, image, want)
		})
	}
}

func riscv32ExportedCallModule(t *testing.T) *wasm.Module {
	t.Helper()
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I64}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 1, 0x0b}))),
	))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCompileModuleCallsExportThroughSerializedEntryThunkUnderQEMU(t *testing.T) {
	qemu, err := exec.LookPath("qemu-riscv32")
	if err != nil {
		t.Skip("qemu-riscv32 not installed")
	}
	cm, err := CompileModule(riscv32ExportedCallModule(t))
	if err != nil {
		t.Fatal(err)
	}
	meta := cm.Functions[0]
	if !meta.HasCallEntry || meta.CallOffset%16 != 0 || meta.ParamSlots != 3 || meta.ResultSlots != 2 {
		t.Fatalf("function metadata=%+v", meta)
	}
	var a rv.Asm
	rvMemoryContext(&a)
	a.MovImm32(rv.T0, 9)
	a.Sw(rv.T0, rv.SP, 64)
	a.MovImm32(rv.T0, 42)
	a.Sw(rv.T0, rv.SP, 68)
	a.Sw(rv.Zero, rv.SP, 72)
	a.Sw(rv.Zero, rv.SP, 80)
	a.Sw(rv.Zero, rv.SP, 84)
	a.Addi(rv.T0, rv.SP, 16)
	a.Sw(rv.T0, rv.SP, 96)
	a.Addi(rv.T0, rv.SP, 64)
	a.Sw(rv.T0, rv.SP, 100)
	a.Addi(rv.T0, rv.SP, 80)
	a.Sw(rv.T0, rv.SP, 104)
	a.Addi(rv.A0, rv.SP, 96)
	call := a.Jal(rv.RA)
	a.Lw(rv.A0, rv.SP, 80)
	a.MovImm32(rv.A7, 93)
	a.Ecall()
	if !a.PatchJAL21(call, len(a.B)+int(meta.CallOffset)) {
		t.Fatal("export thunk relocation")
	}
	runRV32Exit(t, qemu, append(a.B, cm.Code...), 42)
}

func riscv32StartModule(t *testing.T) *wasm.Module {
	t.Helper()
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 0, 0x0b}))),
		wasmtest.Section(8, []byte{0}),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 42, 0x24, 0, 0x0b}))),
	))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCompileModuleInvokesStartThroughEntryThunkUnderQEMU(t *testing.T) {
	qemu, err := exec.LookPath("qemu-riscv32")
	if err != nil {
		t.Skip("qemu-riscv32 not installed")
	}
	cm, err := CompileModule(riscv32StartModule(t))
	if err != nil {
		t.Fatal(err)
	}
	if cm.StartEntry == nil || *cm.StartEntry%16 != 0 {
		t.Fatalf("start entry=%v", cm.StartEntry)
	}
	var a rv.Asm
	rvMemoryContext(&a)
	a.Addi(rv.A0, rv.SP, 16)
	call := a.Jal(rv.RA)
	a.Lw(rv.A0, rv.SP, 60)
	a.MovImm32(rv.A7, 93)
	a.Ecall()
	if !a.PatchJAL21(call, len(a.B)+*cm.StartEntry) {
		t.Fatal("start thunk relocation")
	}
	runRV32Exit(t, qemu, append(a.B, cm.Code...), 42)
}

func riscv32GlobalModule(t *testing.T) *wasm.Module {
	t.Helper()
	global := wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 7, 0x0b})
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(6, wasmtest.Vec(global)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x24, 0, 0x23, 0, 0x0b}))),
	))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCompileModuleI32GlobalsUnderQEMU(t *testing.T) {
	qemu, err := exec.LookPath("qemu-riscv32")
	if err != nil {
		t.Skip("qemu-riscv32 not installed")
	}
	cm, err := CompileModule(riscv32GlobalModule(t))
	if err != nil {
		t.Fatal(err)
	}
	cells := make([]uint32, 1)
	if err := cm.InstantiateGlobals(cells); err != nil || cells[0] != 7 {
		t.Fatalf("globals=%v err=%v", cells, err)
	}
	meta := cm.Functions[0]
	fn := cm.Code[meta.Offset : meta.Offset+meta.Size]
	var a rv.Asm
	rvMemoryContext(&a)
	a.MovImm32(rv.T1, cells[0])
	a.Sw(rv.T1, rv.SP, 60)
	a.Addi(rv.A0, rv.SP, 16)
	a.MovReg(rv.X23, rv.A0)
	a.MovImm32(rv.A0, 42)
	call := a.Jal(rv.RA)
	a.MovImm32(rv.A7, 93)
	a.Ecall()
	a.PatchJAL21(call, len(a.B))
	runRV32Exit(t, qemu, append(a.B, fn...), 42)
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
	t.Run("stack-overflow", func(t *testing.T) {
		var a rv.Asm
		rvMemoryContext(&a)
		a.Sw(rv.SP, rv.SP, 40)
		a.Addi(rv.A0, rv.SP, 16)
		a.MovReg(rv.X23, rv.A0)
		a.MovImm32(rv.A0, 4)
		call := a.Jal(rv.RA)
		a.Lw(rv.A0, rv.SP, 32)
		a.MovImm32(rv.A7, 93)
		a.Ecall()
		a.PatchJAL21(call, len(a.B))
		runRV32Exit(t, qemu, append(a.B, fn...), int(embedded32.TrapStackOverflow))
	})
	t.Run("canceled-entry", func(t *testing.T) {
		var a rv.Asm
		rvMemoryContext(&a)
		a.MovImm32(rv.T1, 1)
		a.Sw(rv.T1, rv.SP, 56)
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
