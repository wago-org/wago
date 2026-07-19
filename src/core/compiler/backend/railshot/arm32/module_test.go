package arm32

import (
	"encoding/binary"
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
	armMemoryContext(&wrapper)
	armContextArg(&wrapper)
	wrapper.MovReg(a32.R11, a32.R0)
	call := wrapper.Call()
	armExit(&wrapper)
	wrapper.PatchCall(call, len(wrapper.B))
	runARM32Exit(t, qemu, append(wrapper.B, fn...), 42)
}

func arm32GenuinelyMixedModule(t *testing.T) *wasm.Module {
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
	qemu, err := exec.LookPath("qemu-arm")
	if err != nil {
		t.Skip("qemu-arm not installed")
	}
	cm, err := CompileModule(arm32GenuinelyMixedModule(t))
	if err != nil {
		t.Fatal(err)
	}
	if cm.Functions[0].ParamSlots != 3 || cm.Functions[0].ResultSlots != 2 {
		t.Fatalf("metadata=%+v", cm.Functions[0])
	}
	meta := cm.Functions[0]
	fn := cm.Code[meta.Offset : meta.Offset+meta.Size]
	var a a32.Asm
	armMemoryContext(&a)
	armContextArg(&a)
	a.MovReg(a32.R11, a32.R0)
	a.MovImm32(a32.R0, 7)
	a.MovImm32(a32.R1, 37)
	a.MovImm32(a32.R2, 0)
	call := a.Call()
	armExit(&a)
	a.PatchCall(call, len(a.B))
	runARM32Exit(t, qemu, append(a.B, fn...), 42)
}

func arm32MixedCallModule(t *testing.T) *wasm.Module {
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
	qemu, err := exec.LookPath("qemu-arm")
	if err != nil {
		t.Skip("qemu-arm not installed")
	}
	cm, err := CompileModule(arm32MixedCallModule(t))
	if err != nil {
		t.Fatal(err)
	}
	var a a32.Asm
	armMemoryContext(&a)
	armContextArg(&a)
	a.MovReg(a32.R11, a32.R0)
	a.MovImm32(a32.R0, 7)
	a.MovImm32(a32.R1, 37)
	a.MovImm32(a32.R2, 0)
	call := a.Call()
	armExit(&a)
	if !a.PatchCall(call, len(a.B)+cm.Entry[1]) {
		t.Fatal("wrapper call relocation")
	}
	image := append(a.B, cm.Code...)
	runARM32Exit(t, qemu, image, 142)
}

func arm32MixedTrapCallModule(t *testing.T) *wasm.Module {
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
	qemu, err := exec.LookPath("qemu-arm")
	if err != nil {
		t.Skip("qemu-arm not installed")
	}
	cm, err := CompileModule(arm32MixedTrapCallModule(t))
	if err != nil {
		t.Fatal(err)
	}
	var a a32.Asm
	armMemoryContext(&a)
	armContextArg(&a)
	a.MovReg(a32.R11, a32.R0)
	a.MovImm32(a32.R0, 7)
	a.MovImm32(a32.R1, 37)
	a.MovImm32(a32.R2, 0)
	call := a.Call()
	a.Ldr(a32.R0, a32.SP, 32)
	armExit(&a)
	if !a.PatchCall(call, len(a.B)+cm.Entry[1]) {
		t.Fatal("wrapper call relocation")
	}
	runARM32Exit(t, qemu, append(a.B, cm.Code...), int(embedded32.TrapUnreachable))
}

func arm32MixedMultiResultCallModule(t *testing.T) *wasm.Module {
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
	qemu, err := exec.LookPath("qemu-arm")
	if err != nil {
		t.Skip("qemu-arm not installed")
	}
	cm, err := CompileModule(arm32MixedMultiResultCallModule(t))
	if err != nil {
		t.Fatal(err)
	}
	if cm.Functions[1].ResultSlots != 3 {
		t.Fatalf("metadata=%+v", cm.Functions[1])
	}
	var a a32.Asm
	armMemoryContext(&a)
	armContextArg(&a)
	a.MovReg(a32.R11, a32.R0)
	a.MovImm32(a32.R0, 42)
	a.MovImm32(a32.R1, 37)
	a.MovImm32(a32.R2, 0)
	call := a.Call()
	a.Add(a32.R0, a32.R0, a32.R1)
	armExit(&a)
	if !a.PatchCall(call, len(a.B)+cm.Entry[1]) {
		t.Fatal("wrapper call relocation")
	}
	runARM32Exit(t, qemu, append(a.B, cm.Code...), 79)
}

func arm32MixedSelectModule(t *testing.T) *wasm.Module {
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
	qemu, err := exec.LookPath("qemu-arm")
	if err != nil {
		t.Skip("qemu-arm not installed")
	}
	cm, err := CompileModule(arm32MixedSelectModule(t))
	if err != nil {
		t.Fatal(err)
	}
	var a a32.Asm
	armMemoryContext(&a)
	armContextArg(&a)
	a.MovReg(a32.R11, a32.R0)
	call := a.Call()
	armExit(&a)
	if !a.PatchCall(call, len(a.B)+cm.Entry[0]) {
		t.Fatal("wrapper call relocation")
	}
	runARM32Exit(t, qemu, append(a.B, cm.Code...), 42)
}

func arm32MixedStackABIModule(t *testing.T) *wasm.Module {
	t.Helper()
	sig := wasmtest.FuncType(
		[]wasm.ValType{wasm.I32, wasm.I64, wasm.I64},
		[]wasm.ValType{wasm.I64, wasm.I64, wasm.I32},
	)
	callee := []byte{0x20, 1, 0x20, 2, 0x20, 0, 0x0b}
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
	qemu, err := exec.LookPath("qemu-arm")
	if err != nil {
		t.Skip("qemu-arm not installed")
	}
	cm, err := CompileModule(arm32MixedStackABIModule(t))
	if err != nil {
		t.Fatal(err)
	}
	if cm.Functions[1].ParamSlots != 5 || cm.Functions[1].ResultSlots != 5 {
		t.Fatalf("metadata=%+v", cm.Functions[1])
	}
	var a a32.Asm
	armMemoryContext(&a)
	a.MovImm32(a32.R12, 0)
	a.Str(a32.R12, a32.SP, 0)
	armContextArg(&a)
	a.MovReg(a32.R11, a32.R0)
	a.MovImm32(a32.R0, 7)
	a.MovImm32(a32.R1, 37)
	a.MovImm32(a32.R2, 0)
	a.MovImm32(a32.R3, 5)
	call := a.Call()
	a.Add(a32.R0, a32.R0, a32.R2)
	a.Ldr(a32.R1, a32.SP, 0)
	a.Add(a32.R0, a32.R0, a32.R1)
	armExit(&a)
	if !a.PatchCall(call, len(a.B)+cm.Entry[1]) {
		t.Fatal("wrapper call relocation")
	}
	runARM32Exit(t, qemu, append(a.B, cm.Code...), 49)
}

func arm32MixedIfModule(t *testing.T) *wasm.Module {
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
	qemu, err := exec.LookPath("qemu-arm")
	if err != nil {
		t.Skip("qemu-arm not installed")
	}
	cm, err := CompileModule(arm32MixedIfModule(t))
	if err != nil {
		t.Fatal(err)
	}
	var a a32.Asm
	armMemoryContext(&a)
	armContextArg(&a)
	a.MovReg(a32.R11, a32.R0)
	call := a.Call()
	armExit(&a)
	if !a.PatchCall(call, len(a.B)+cm.Entry[0]) {
		t.Fatal("wrapper call relocation")
	}
	runARM32Exit(t, qemu, append(a.B, cm.Code...), 42)
}

func arm32MixedLoopModule(t *testing.T) *wasm.Module {
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
	qemu, err := exec.LookPath("qemu-arm")
	if err != nil {
		t.Skip("qemu-arm not installed")
	}
	cm, err := CompileModule(arm32MixedLoopModule(t))
	if err != nil {
		t.Fatal(err)
	}
	var a a32.Asm
	armMemoryContext(&a)
	armContextArg(&a)
	a.MovReg(a32.R11, a32.R0)
	call := a.Call()
	armExit(&a)
	if !a.PatchCall(call, len(a.B)+cm.Entry[0]) {
		t.Fatal("wrapper call relocation")
	}
	runARM32Exit(t, qemu, append(a.B, cm.Code...), 43)
}

func arm32MixedGlobalModule(t *testing.T) *wasm.Module {
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
	qemu, err := exec.LookPath("qemu-arm")
	if err != nil {
		t.Skip("qemu-arm not installed")
	}
	cm, err := CompileModule(arm32MixedGlobalModule(t))
	if err != nil {
		t.Fatal(err)
	}
	var a a32.Asm
	armMemoryContext(&a)
	a.MovImm32(a32.R12, 40)
	a.Str(a32.R12, a32.SP, 60)
	armContextArg(&a)
	a.MovReg(a32.R11, a32.R0)
	call := a.Call()
	armExit(&a)
	if !a.PatchCall(call, len(a.B)+cm.Entry[0]) {
		t.Fatal("wrapper call relocation")
	}
	runARM32Exit(t, qemu, append(a.B, cm.Code...), 42)
}

func arm32MixedMemorySizeGrowModule(t *testing.T) *wasm.Module {
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
	qemu, err := exec.LookPath("qemu-arm")
	if err != nil {
		t.Skip("qemu-arm not installed")
	}
	cm, err := CompileModule(arm32MixedMemorySizeGrowModule(t))
	if err != nil {
		t.Fatal(err)
	}
	var a a32.Asm
	armMemoryContext(&a)
	a.MovImm32(a32.R12, 65536)
	a.Str(a32.R12, a32.SP, 20)
	a.Str(a32.R12, a32.SP, 36)
	armContextArg(&a)
	a.MovReg(a32.R11, a32.R0)
	call := a.Call()
	armExit(&a)
	if !a.PatchCall(call, len(a.B)+cm.Entry[0]) {
		t.Fatal("wrapper call relocation")
	}
	runARM32Exit(t, qemu, append(a.B, cm.Code...), 2)
}

func arm32WideGlobalModule(t *testing.T) *wasm.Module {
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
	qemu, err := exec.LookPath("qemu-arm")
	if err != nil {
		t.Skip("qemu-arm not installed")
	}
	cm, err := CompileModule(arm32WideGlobalModule(t))
	if err != nil {
		t.Fatal(err)
	}
	cells := make([]uint32, 2)
	if err := cm.InstantiateGlobals(cells); err != nil || cells[0] != 40 || cells[1] != 0 {
		t.Fatalf("globals=%v err=%v", cells, err)
	}
	var a a32.Asm
	armMemoryContext(&a)
	a.MovImm32(a32.R12, 72)
	a.Add(a32.R5, a32.SP, a32.R12)
	a.Str(a32.R5, a32.SP, 44)
	a.MovImm32(a32.R12, cells[0])
	a.Str(a32.R12, a32.SP, 72)
	a.MovImm32(a32.R12, cells[1])
	a.Str(a32.R12, a32.SP, 76)
	armContextArg(&a)
	a.MovReg(a32.R11, a32.R0)
	call := a.Call()
	armExit(&a)
	if !a.PatchCall(call, len(a.B)+cm.Entry[0]) {
		t.Fatal("wrapper call relocation")
	}
	runARM32Exit(t, qemu, append(a.B, cm.Code...), 42)
}

func arm32MixedI32CompareModule(t *testing.T) *wasm.Module {
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
	qemu, err := exec.LookPath("qemu-arm")
	if err != nil {
		t.Skip("qemu-arm not installed")
	}
	cm, err := CompileModule(arm32MixedI32CompareModule(t))
	if err != nil {
		t.Fatal(err)
	}
	var a a32.Asm
	armMemoryContext(&a)
	armContextArg(&a)
	a.MovReg(a32.R11, a32.R0)
	call := a.Call()
	armExit(&a)
	if !a.PatchCall(call, len(a.B)+cm.Entry[0]) {
		t.Fatal("wrapper call relocation")
	}
	runARM32Exit(t, qemu, append(a.B, cm.Code...), 4)
}

func arm32ReferenceModule(t *testing.T) *wasm.Module {
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
	qemu, err := exec.LookPath("qemu-arm")
	if err != nil {
		t.Skip("qemu-arm not installed")
	}
	cm, err := CompileModule(arm32ReferenceModule(t))
	if err != nil {
		t.Fatal(err)
	}
	var a a32.Asm
	armMemoryContext(&a)
	armContextArg(&a)
	a.MovReg(a32.R11, a32.R0)
	a.MovImm32(a32.R0, 0)
	first := a.Call()
	a.MovReg(a32.R4, a32.R0)
	a.MovImm32(a32.R0, 7)
	second := a.Call()
	a.Add(a32.R0, a32.R0, a32.R4)
	armExit(&a)
	target := len(a.B) + cm.Entry[0]
	if !a.PatchCall(first, target) || !a.PatchCall(second, target) {
		t.Fatal("wrapper call relocation")
	}
	runARM32Exit(t, qemu, append(a.B, cm.Code...), 1)
}

func TestCompileModuleExecutesRefNullUnderQEMU(t *testing.T) {
	qemu, err := exec.LookPath("qemu-arm")
	if err != nil {
		t.Skip("qemu-arm not installed")
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
	var a a32.Asm
	armMemoryContext(&a)
	armContextArg(&a)
	a.MovReg(a32.R11, a32.R0)
	call := a.Call()
	armExit(&a)
	if !a.PatchCall(call, len(a.B)+cm.Entry[0]) {
		t.Fatal("wrapper call relocation")
	}
	runARM32Exit(t, qemu, append(a.B, cm.Code...), 1)
}

func arm32TableAccessModule(t *testing.T) *wasm.Module {
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
	qemu, err := exec.LookPath("qemu-arm")
	if err != nil {
		t.Skip("qemu-arm not installed")
	}
	cm, err := CompileModule(arm32TableAccessModule(t))
	if err != nil {
		t.Fatal(err)
	}
	entries := make([]uint32, 2)
	if err := cm.InstantiateTable(entries); err != nil {
		t.Fatal(err)
	}
	var a a32.Asm
	armMemoryContext(&a)
	a.MovImm32(a32.R12, 92)
	a.Add(a32.R5, a32.SP, a32.R12)
	a.Str(a32.R5, a32.SP, 28)
	a.MovImm32(a32.R12, 64)
	a.Add(a32.R5, a32.SP, a32.R12)
	a.Str(a32.R5, a32.SP, 56)
	a.MovImm32(a32.R12, 96)
	a.Add(a32.R5, a32.SP, a32.R12)
	a.Str(a32.R5, a32.SP, 64)
	a.MovImm32(a32.R12, 2)
	a.Str(a32.R12, a32.SP, 68)
	a.Str(a32.R12, a32.SP, 72)
	a.MovImm32(a32.R12, 0)
	a.Str(a32.R12, a32.SP, 76)
	a.Str(a32.R12, a32.SP, 80)
	a.Str(a32.R12, a32.SP, 84)
	a.Str(a32.R12, a32.SP, 88)
	a.Str(a32.R12, a32.SP, 92)
	a.Str(a32.R12, a32.SP, 96)
	a.Str(a32.R12, a32.SP, 100)
	armContextArg(&a)
	a.MovReg(a32.R11, a32.R0)
	a.MovImm32(a32.R0, 7)
	call := a.Call()
	armExit(&a)
	if !a.PatchCall(call, len(a.B)+cm.Entry[0]) {
		t.Fatal("wrapper call relocation")
	}
	runARM32Exit(t, qemu, append(a.B, cm.Code...), 42)
}

func arm32TableBulkModule(t *testing.T) *wasm.Module {
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
	qemu, err := exec.LookPath("qemu-arm")
	if err != nil {
		t.Skip("qemu-arm not installed")
	}
	cm, err := CompileModule(arm32TableBulkModule(t))
	if err != nil {
		t.Fatal(err)
	}
	var a a32.Asm
	armMemoryContext(&a)
	a.MovImm32(a32.R12, 124)
	a.Add(a32.R5, a32.SP, a32.R12)
	a.Str(a32.R5, a32.SP, 28)
	a.MovImm32(a32.R12, 64)
	a.Add(a32.R5, a32.SP, a32.R12)
	a.Str(a32.R5, a32.SP, 56)
	a.MovImm32(a32.R12, 96)
	a.Add(a32.R5, a32.SP, a32.R12)
	a.Str(a32.R5, a32.SP, 64)
	a.MovImm32(a32.R12, 2)
	a.Str(a32.R12, a32.SP, 68)
	a.MovImm32(a32.R12, 4)
	a.Str(a32.R12, a32.SP, 72)
	a.MovImm32(a32.R12, 0)
	a.Str(a32.R12, a32.SP, 76)
	a.Str(a32.R12, a32.SP, 80)
	a.Str(a32.R12, a32.SP, 84)
	a.Str(a32.R12, a32.SP, 88)
	a.Str(a32.R12, a32.SP, 92)
	a.Str(a32.R12, a32.SP, 96)
	a.Str(a32.R12, a32.SP, 100)
	a.Str(a32.R12, a32.SP, 104)
	a.Str(a32.R12, a32.SP, 108)
	a.Str(a32.R12, a32.SP, 124)
	armContextArg(&a)
	a.MovReg(a32.R11, a32.R0)
	a.MovImm32(a32.R0, 7)
	call := a.Call()
	armExit(&a)
	if !a.PatchCall(call, len(a.B)+cm.Entry[0]) {
		t.Fatal("wrapper call relocation")
	}
	runARM32Exit(t, qemu, append(a.B, cm.Code...), 6)
}

func arm32TableInitModule(t *testing.T) *wasm.Module {
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
	qemu, err := exec.LookPath("qemu-arm")
	if err != nil {
		t.Skip("qemu-arm not installed")
	}
	cm, err := CompileModule(arm32TableInitModule(t))
	if err != nil {
		t.Fatal(err)
	}
	segments, err := cm.ElementSegmentABI([]uint32{0x1234})
	if err != nil || len(segments) != 1 || segments[0].Dropped != 0 || segments[0].Length != 1 {
		t.Fatalf("element segments=%v err=%v", segments, err)
	}
	var a a32.Asm
	armMemoryContext(&a)
	a.MovImm32(a32.R12, 124)
	a.Add(a32.R5, a32.SP, a32.R12)
	a.Str(a32.R5, a32.SP, 28)
	a.MovImm32(a32.R12, 64)
	a.Add(a32.R5, a32.SP, a32.R12)
	a.Str(a32.R5, a32.SP, 56)
	a.MovImm32(a32.R12, 96)
	a.Add(a32.R5, a32.SP, a32.R12)
	a.Str(a32.R5, a32.SP, 64)
	a.MovImm32(a32.R12, 1)
	a.Str(a32.R12, a32.SP, 68)
	a.Str(a32.R12, a32.SP, 72)
	a.MovImm32(a32.R12, 0)
	a.Str(a32.R12, a32.SP, 76)
	a.Str(a32.R12, a32.SP, 80)
	a.MovImm32(a32.R12, 104)
	a.Add(a32.R5, a32.SP, a32.R12)
	a.Str(a32.R5, a32.SP, 84)
	a.MovImm32(a32.R12, 1)
	a.Str(a32.R12, a32.SP, 88)
	a.MovImm32(a32.R12, 0)
	a.Str(a32.R12, a32.SP, 96)
	a.MovImm32(a32.R12, 120)
	a.Add(a32.R5, a32.SP, a32.R12)
	a.Str(a32.R5, a32.SP, 104)
	a.MovImm32(a32.R12, 1)
	a.Str(a32.R12, a32.SP, 108)
	a.MovImm32(a32.R12, 0)
	a.Str(a32.R12, a32.SP, 112)
	a.MovImm32(a32.R12, 1)
	a.Str(a32.R12, a32.SP, 120)
	a.MovImm32(a32.R12, 0)
	a.Str(a32.R12, a32.SP, 124)
	armContextArg(&a)
	a.MovReg(a32.R11, a32.R0)
	call := a.Call()
	armExit(&a)
	if !a.PatchCall(call, len(a.B)+cm.Entry[1]) {
		t.Fatal("wrapper call relocation")
	}
	runARM32Exit(t, qemu, append(a.B, cm.Code...), 42)
}

func arm32IndirectCallModule(t *testing.T) *wasm.Module {
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
	qemu, err := exec.LookPath("qemu-arm")
	if err != nil {
		t.Skip("qemu-arm not installed")
	}
	cm, err := CompileModule(arm32IndirectCallModule(t))
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
	buildWrapper := func(fn0, fn1 uint32) a32.Asm {
		var a a32.Asm
		armMemoryContext(&a)
		a.MovImm32(a32.R12, 124)
		a.Add(a32.R5, a32.SP, a32.R12)
		a.Str(a32.R5, a32.SP, 28)
		a.MovImm32(a32.R12, 64)
		a.Add(a32.R5, a32.SP, a32.R12)
		a.Str(a32.R5, a32.SP, 56)
		a.MovImm32(a32.R12, 96)
		a.Add(a32.R5, a32.SP, a32.R12)
		a.Str(a32.R5, a32.SP, 64)
		a.MovImm32(a32.R12, 1)
		a.Str(a32.R12, a32.SP, 68)
		a.Str(a32.R12, a32.SP, 72)
		a.MovImm32(a32.R12, 100)
		a.Add(a32.R5, a32.SP, a32.R12)
		a.Str(a32.R5, a32.SP, 76)
		a.MovImm32(a32.R12, 112)
		a.Add(a32.R5, a32.SP, a32.R12)
		a.Str(a32.R5, a32.SP, 80)
		a.MovImm32(a32.R12, 0)
		a.Str(a32.R12, a32.SP, 84)
		a.Str(a32.R12, a32.SP, 88)
		a.MovImm32(a32.R12, entries[0])
		a.Str(a32.R12, a32.SP, 96)
		a.MovImm32(a32.R12, fn0|1)
		a.Str(a32.R12, a32.SP, 100)
		a.MovImm32(a32.R12, fn1|1)
		a.Str(a32.R12, a32.SP, 104)
		a.MovImm32(a32.R12, cm.FunctionTypeIDs[0])
		a.Str(a32.R12, a32.SP, 112)
		a.MovImm32(a32.R12, cm.FunctionTypeIDs[1])
		a.Str(a32.R12, a32.SP, 116)
		a.MovImm32(a32.R12, 0)
		a.Str(a32.R12, a32.SP, 124)
		armContextArg(&a)
		a.MovReg(a32.R11, a32.R0)
		a.MovImm32(a32.R0, 41)
		call := a.Call()
		armExit(&a)
		if !a.PatchCall(call, len(a.B)+cm.Entry[1]) {
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
	runARM32Exit(t, qemu, append(wrapper.B, cm.Code...), 42)
}

func arm32MixedF64HelperModule(t *testing.T) *wasm.Module {
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
	qemu, err := exec.LookPath("qemu-arm")
	if err != nil {
		t.Skip("qemu-arm not installed")
	}
	cm, err := CompileModule(arm32MixedF64HelperModule(t))
	if err != nil {
		t.Fatal(err)
	}
	var helper a32.Asm
	helper.Ldr(a32.R1, a32.R0, embedded32.F64FrameALoOffset)
	helper.Ldr(a32.R2, a32.R0, embedded32.F64FrameBLoOffset)
	helper.Add(a32.R1, a32.R1, a32.R2)
	helper.Str(a32.R1, a32.R0, embedded32.F64FrameOutLoOffset)
	helper.MovImm32(a32.R2, 0)
	helper.Str(a32.R2, a32.R0, embedded32.F64FrameOutHiOffset)
	helper.Str(a32.R2, a32.R0, embedded32.F64FrameTrapOffset)
	helper.Ret()
	helper.Align4()
	buildWrapper := func(table uint32) a32.Asm {
		var a a32.Asm
		armMemoryContext(&a)
		a.MovImm32(a32.R12, 84)
		a.Add(a32.R5, a32.SP, a32.R12)
		a.Str(a32.R5, a32.SP, 24)
		a.MovImm32(a32.R12, 0)
		a.Str(a32.R12, a32.SP, 84)
		a.MovImm32(a32.R12, table)
		a.Str(a32.R12, a32.SP, 32)
		armContextArg(&a)
		a.MovReg(a32.R11, a32.R0)
		call := a.Call()
		armExit(&a)
		if table != 0 && !a.PatchCall(call, len(a.B)+cm.Entry[0]) {
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
	binary.LittleEndian.PutUint32(table[embedded32.HelperF64Offset:], base|uint32(helperOff)|1)
	image = append(image, table[:]...)
	runARM32Exit(t, qemu, image, 42)
}

func arm32MixedI64MultiplyModule(t *testing.T) *wasm.Module {
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
	qemu, err := exec.LookPath("qemu-arm")
	if err != nil {
		t.Skip("qemu-arm not installed")
	}
	cm, err := CompileModule(arm32MixedI64MultiplyModule(t))
	if err != nil {
		t.Fatal(err)
	}
	var a a32.Asm
	armMemoryContext(&a)
	armContextArg(&a)
	a.MovReg(a32.R11, a32.R0)
	call := a.Call()
	armExit(&a)
	if !a.PatchCall(call, len(a.B)+cm.Entry[0]) {
		t.Fatal("wrapper call relocation")
	}
	runARM32Exit(t, qemu, append(a.B, cm.Code...), 42)
}

func arm32MixedI64HelperModule(t *testing.T) *wasm.Module {
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
	qemu, err := exec.LookPath("qemu-arm")
	if err != nil {
		t.Skip("qemu-arm not installed")
	}
	cm, err := CompileModule(arm32MixedI64HelperModule(t))
	if err != nil {
		t.Fatal(err)
	}
	var helper a32.Asm
	helper.Ldr(a32.R1, a32.R0, embedded32.I64FrameALoOffset)
	helper.Ldr(a32.R2, a32.R0, embedded32.I64FrameBLoOffset)
	helper.Add(a32.R1, a32.R1, a32.R2)
	helper.Str(a32.R1, a32.R0, embedded32.I64FrameOutLoOffset)
	helper.MovImm32(a32.R2, 0)
	helper.Str(a32.R2, a32.R0, embedded32.I64FrameOutHiOffset)
	helper.Str(a32.R2, a32.R0, embedded32.I64FrameTrapOffset)
	helper.Ret()
	helper.Align4()
	buildWrapper := func(table uint32) a32.Asm {
		var a a32.Asm
		armMemoryContext(&a)
		a.MovImm32(a32.R12, 84)
		a.Add(a32.R5, a32.SP, a32.R12)
		a.Str(a32.R5, a32.SP, 24)
		a.MovImm32(a32.R12, 0)
		a.Str(a32.R12, a32.SP, 84)
		a.MovImm32(a32.R12, table)
		a.Str(a32.R12, a32.SP, 32)
		armContextArg(&a)
		a.MovReg(a32.R11, a32.R0)
		call := a.Call()
		armExit(&a)
		if !a.PatchCall(call, len(a.B)+cm.Entry[0]) {
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
	binary.LittleEndian.PutUint32(table[embedded32.HelperI64Offset:], base|uint32(helperOff)|1)
	image = append(image, table[:]...)
	runARM32Exit(t, qemu, image, 42)
}

func arm32MixedMemoryModule(t *testing.T) *wasm.Module {
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
	qemu, err := exec.LookPath("qemu-arm")
	if err != nil {
		t.Skip("qemu-arm not installed")
	}
	cm, err := CompileModule(arm32MixedMemoryModule(t))
	if err != nil {
		t.Fatal(err)
	}
	var a a32.Asm
	armMemoryContext(&a)
	armContextArg(&a)
	a.MovReg(a32.R11, a32.R0)
	call := a.Call()
	armExit(&a)
	if !a.PatchCall(call, len(a.B)+cm.Entry[0]) {
		t.Fatal("wrapper call relocation")
	}
	runARM32Exit(t, qemu, append(a.B, cm.Code...), 42)
}

func arm32MixedTypedIfModule(t *testing.T) *wasm.Module {
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
	qemu, err := exec.LookPath("qemu-arm")
	if err != nil {
		t.Skip("qemu-arm not installed")
	}
	cm, err := CompileModule(arm32MixedTypedIfModule(t))
	if err != nil {
		t.Fatal(err)
	}
	var a a32.Asm
	armMemoryContext(&a)
	armContextArg(&a)
	a.MovReg(a32.R11, a32.R0)
	call := a.Call()
	armExit(&a)
	if !a.PatchCall(call, len(a.B)+cm.Entry[0]) {
		t.Fatal("wrapper call relocation")
	}
	runARM32Exit(t, qemu, append(a.B, cm.Code...), 42)
}

func arm32MixedMultiValueIfModule(t *testing.T) *wasm.Module {
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
	qemu, err := exec.LookPath("qemu-arm")
	if err != nil {
		t.Skip("qemu-arm not installed")
	}
	cm, err := CompileModule(arm32MixedMultiValueIfModule(t))
	if err != nil {
		t.Fatal(err)
	}
	var a a32.Asm
	armMemoryContext(&a)
	armContextArg(&a)
	a.MovReg(a32.R11, a32.R0)
	call := a.Call()
	a.Add(a32.R0, a32.R0, a32.R2)
	armExit(&a)
	if !a.PatchCall(call, len(a.B)+cm.Entry[0]) {
		t.Fatal("wrapper call relocation")
	}
	runARM32Exit(t, qemu, append(a.B, cm.Code...), 44)
}

func arm32MixedParameterizedIfModule(t *testing.T) *wasm.Module {
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
	qemu, err := exec.LookPath("qemu-arm")
	if err != nil {
		t.Skip("qemu-arm not installed")
	}
	cm, err := CompileModule(arm32MixedParameterizedIfModule(t))
	if err != nil {
		t.Fatal(err)
	}
	var a a32.Asm
	armMemoryContext(&a)
	armContextArg(&a)
	a.MovReg(a32.R11, a32.R0)
	call := a.Call()
	armExit(&a)
	if !a.PatchCall(call, len(a.B)+cm.Entry[0]) {
		t.Fatal("wrapper call relocation")
	}
	runARM32Exit(t, qemu, append(a.B, cm.Code...), 42)
}

func arm32MixedTypedLoopModule(t *testing.T) *wasm.Module {
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
	qemu, err := exec.LookPath("qemu-arm")
	if err != nil {
		t.Skip("qemu-arm not installed")
	}
	cm, err := CompileModule(arm32MixedTypedLoopModule(t))
	if err != nil {
		t.Fatal(err)
	}
	var a a32.Asm
	armMemoryContext(&a)
	armContextArg(&a)
	a.MovReg(a32.R11, a32.R0)
	call := a.Call()
	armExit(&a)
	if !a.PatchCall(call, len(a.B)+cm.Entry[0]) {
		t.Fatal("wrapper call relocation")
	}
	runARM32Exit(t, qemu, append(a.B, cm.Code...), 42)
}

func arm32MixedBranchModule(t *testing.T) *wasm.Module {
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
	qemu, err := exec.LookPath("qemu-arm")
	if err != nil {
		t.Skip("qemu-arm not installed")
	}
	cm, err := CompileModule(arm32MixedBranchModule(t))
	if err != nil {
		t.Fatal(err)
	}
	var a a32.Asm
	armMemoryContext(&a)
	armContextArg(&a)
	a.MovReg(a32.R11, a32.R0)
	call := a.Call()
	armExit(&a)
	if !a.PatchCall(call, len(a.B)+cm.Entry[0]) {
		t.Fatal("wrapper call relocation")
	}
	runARM32Exit(t, qemu, append(a.B, cm.Code...), 42)
}

func arm32MixedSIMDHelperModule(t *testing.T) *wasm.Module {
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
	qemu, err := exec.LookPath("qemu-arm")
	if err != nil {
		t.Skip("qemu-arm not installed")
	}
	cm, err := CompileModule(arm32MixedSIMDHelperModule(t))
	if err != nil {
		t.Fatal(err)
	}
	var helper a32.Asm
	helper.Ldr(a32.R1, a32.R0, embedded32.SIMDFrameAOffset)
	helper.Ldr(a32.R2, a32.R0, embedded32.SIMDFrameBOffset)
	helper.Add(a32.R1, a32.R1, a32.R2)
	helper.Str(a32.R1, a32.R0, embedded32.SIMDFrameOutOffset)
	helper.MovImm32(a32.R2, 0)
	for i := uint16(1); i < 4; i++ {
		helper.Str(a32.R2, a32.R0, embedded32.SIMDFrameOutOffset+i*4)
	}
	helper.Str(a32.R2, a32.R0, embedded32.SIMDFrameTrapOffset)
	helper.Ret()
	helper.Align4()
	buildWrapper := func(table uint32) a32.Asm {
		var a a32.Asm
		armMemoryContext(&a)
		a.MovImm32(a32.R12, 84)
		a.Add(a32.R5, a32.SP, a32.R12)
		a.Str(a32.R5, a32.SP, 24)
		a.MovImm32(a32.R12, 0)
		a.Str(a32.R12, a32.SP, 84)
		a.MovImm32(a32.R12, table)
		a.Str(a32.R12, a32.SP, 32)
		armContextArg(&a)
		a.MovReg(a32.R11, a32.R0)
		call := a.Call()
		armExit(&a)
		if !a.PatchCall(call, len(a.B)+cm.Entry[0]) {
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
	binary.LittleEndian.PutUint32(table[embedded32.HelperSIMDOffset:], base|uint32(helperOff)|1)
	image = append(image, table[:]...)
	runARM32Exit(t, qemu, image, 42)
}

func arm32MixedSIMDMemoryModule(t *testing.T) *wasm.Module {
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
	qemu, err := exec.LookPath("qemu-arm")
	if err != nil {
		t.Skip("qemu-arm not installed")
	}
	cm, err := CompileModule(arm32MixedSIMDMemoryModule(t))
	if err != nil {
		t.Fatal(err)
	}
	var helper a32.Asm
	helper.Ldr(a32.R1, a32.R0, embedded32.SIMDFrameMemoryBaseOffset)
	helper.Ldr(a32.R2, a32.R0, embedded32.SIMDFrameAddressOffset)
	helper.Add(a32.R1, a32.R1, a32.R2)
	helper.Ldr(a32.R2, a32.R1, 0)
	helper.Str(a32.R2, a32.R0, embedded32.SIMDFrameOutOffset)
	helper.MovImm32(a32.R2, 0)
	for i := uint16(1); i < 4; i++ {
		helper.Str(a32.R2, a32.R0, embedded32.SIMDFrameOutOffset+i*4)
	}
	helper.Str(a32.R2, a32.R0, embedded32.SIMDFrameTrapOffset)
	helper.Ret()
	helper.Align4()
	buildWrapper := func(table uint32) a32.Asm {
		var a a32.Asm
		armMemoryContext(&a)
		a.MovImm32(a32.R12, 42)
		a.Str(a32.R12, a32.SP, 0)
		a.MovImm32(a32.R12, 84)
		a.Add(a32.R5, a32.SP, a32.R12)
		a.Str(a32.R5, a32.SP, 24)
		a.MovImm32(a32.R12, 0)
		a.Str(a32.R12, a32.SP, 84)
		a.MovImm32(a32.R12, table)
		a.Str(a32.R12, a32.SP, 32)
		armContextArg(&a)
		a.MovReg(a32.R11, a32.R0)
		call := a.Call()
		armExit(&a)
		if !a.PatchCall(call, len(a.B)+cm.Entry[0]) {
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
	binary.LittleEndian.PutUint32(table[embedded32.HelperSIMDOffset:], base|uint32(helperOff)|1)
	image = append(image, table[:]...)
	runARM32Exit(t, qemu, image, 42)
}

func arm32MixedF32HelperModule(t *testing.T) *wasm.Module {
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
	qemu, err := exec.LookPath("qemu-arm")
	if err != nil {
		t.Skip("qemu-arm not installed")
	}
	cm, err := CompileModule(arm32MixedF32HelperModule(t))
	if err != nil {
		t.Fatal(err)
	}
	var helper a32.Asm
	helper.Ldr(a32.R1, a32.R0, embedded32.F32FrameALoOffset)
	helper.Ldr(a32.R2, a32.R0, embedded32.F32FrameBLoOffset)
	helper.Add(a32.R1, a32.R1, a32.R2)
	helper.Str(a32.R1, a32.R0, embedded32.F32FrameOutLoOffset)
	helper.MovImm32(a32.R2, 0)
	helper.Str(a32.R2, a32.R0, embedded32.F32FrameOutHiOffset)
	helper.Str(a32.R2, a32.R0, embedded32.F32FrameTrapOffset)
	helper.Ret()
	helper.Align4()
	buildWrapper := func(table uint32) a32.Asm {
		var a a32.Asm
		armMemoryContext(&a)
		a.MovImm32(a32.R12, 84)
		a.Add(a32.R5, a32.SP, a32.R12)
		a.Str(a32.R5, a32.SP, 24)
		a.MovImm32(a32.R12, 0)
		a.Str(a32.R12, a32.SP, 84)
		a.MovImm32(a32.R12, table)
		a.Str(a32.R12, a32.SP, 32)
		armContextArg(&a)
		a.MovReg(a32.R11, a32.R0)
		call := a.Call()
		armExit(&a)
		if !a.PatchCall(call, len(a.B)+cm.Entry[0]) {
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
	binary.LittleEndian.PutUint32(table[embedded32.HelperF32Offset:], base|uint32(helperOff)|1)
	image = append(image, table[:]...)
	runARM32Exit(t, qemu, image, 42)
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

func arm32MemoryInitModule(t *testing.T) *wasm.Module {
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
	qemu, err := exec.LookPath("qemu-arm")
	if err != nil {
		t.Skip("qemu-arm not installed")
	}
	cm, err := CompileModule(arm32MemoryInitModule(t))
	if err != nil {
		t.Fatal(err)
	}
	meta := cm.Functions[0]
	fn := cm.Code[meta.Offset : meta.Offset+meta.Size]
	var a a32.Asm
	armMemoryContext(&a)
	a.MovImm32(a32.R12, 80)
	a.Add(a32.R5, a32.SP, a32.R12)
	a.Str(a32.R5, a32.SP, 64)
	a.MovImm32(a32.R12, 3)
	a.Str(a32.R12, a32.SP, 68)
	a.MovImm32(a32.R12, 0)
	a.Str(a32.R12, a32.SP, 72)
	a.MovImm32(a32.R12, 0x007a7978)
	a.Str(a32.R12, a32.SP, 80)
	a.MovImm32(a32.R12, 1)
	a.Str(a32.R12, a32.SP, 52)
	armContextArg(&a)
	a.MovReg(a32.R11, a32.R0)
	call := a.Call()
	armExit(&a)
	a.PatchCall(call, len(a.B))
	runARM32Exit(t, qemu, append(a.B, fn...), 120)
}

func arm32BulkModule(t *testing.T) *wasm.Module {
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
	qemu, err := exec.LookPath("qemu-arm")
	if err != nil {
		t.Skip("qemu-arm not installed")
	}
	cm, err := CompileModule(arm32BulkModule(t))
	if err != nil {
		t.Fatal(err)
	}
	meta := cm.Functions[0]
	fn := cm.Code[meta.Offset : meta.Offset+meta.Size]
	var a a32.Asm
	armMemoryContext(&a)
	armContextArg(&a)
	a.MovReg(a32.R11, a32.R0)
	call := a.Call()
	armExit(&a)
	a.PatchCall(call, len(a.B))
	runARM32Exit(t, qemu, append(a.B, fn...), 42)
}

func arm32MixedMemoryInitModule(t *testing.T) *wasm.Module {
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
	qemu, err := exec.LookPath("qemu-arm")
	if err != nil {
		t.Skip("qemu-arm not installed")
	}
	cm, err := CompileModule(arm32MixedMemoryInitModule(t))
	if err != nil {
		t.Fatal(err)
	}
	meta := cm.Functions[0]
	fn := cm.Code[meta.Offset : meta.Offset+meta.Size]
	var a a32.Asm
	armMemoryContext(&a)
	a.MovImm32(a32.R12, 80)
	a.Add(a32.R5, a32.SP, a32.R12)
	a.Str(a32.R5, a32.SP, 64)
	a.MovImm32(a32.R12, 3)
	a.Str(a32.R12, a32.SP, 68)
	a.MovImm32(a32.R12, 0)
	a.Str(a32.R12, a32.SP, 72)
	a.MovImm32(a32.R12, 0x007a7978)
	a.Str(a32.R12, a32.SP, 80)
	a.MovImm32(a32.R12, 1)
	a.Str(a32.R12, a32.SP, 52)
	armContextArg(&a)
	a.MovReg(a32.R11, a32.R0)
	call := a.Call()
	armExit(&a)
	a.PatchCall(call, len(a.B))
	runARM32Exit(t, qemu, append(a.B, fn...), 120)
}

func arm32MixedBulkModule(t *testing.T) *wasm.Module {
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
	qemu, err := exec.LookPath("qemu-arm")
	if err != nil {
		t.Skip("qemu-arm not installed")
	}
	cm, err := CompileModule(arm32MixedBulkModule(t))
	if err != nil {
		t.Fatal(err)
	}
	meta := cm.Functions[0]
	fn := cm.Code[meta.Offset : meta.Offset+meta.Size]
	var a a32.Asm
	armMemoryContext(&a)
	armContextArg(&a)
	a.MovReg(a32.R11, a32.R0)
	call := a.Call()
	armExit(&a)
	a.PatchCall(call, len(a.B))
	runARM32Exit(t, qemu, append(a.B, fn...), 42)
}

func arm32GlobalModule(t *testing.T) *wasm.Module {
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
	qemu, err := exec.LookPath("qemu-arm")
	if err != nil {
		t.Skip("qemu-arm not installed")
	}
	cm, err := CompileModule(arm32GlobalModule(t))
	if err != nil {
		t.Fatal(err)
	}
	cells := make([]uint32, 1)
	if err := cm.InstantiateGlobals(cells); err != nil || cells[0] != 7 {
		t.Fatalf("globals=%v err=%v", cells, err)
	}
	meta := cm.Functions[0]
	fn := cm.Code[meta.Offset : meta.Offset+meta.Size]
	var a a32.Asm
	armMemoryContext(&a)
	a.MovImm32(a32.R12, cells[0])
	a.Str(a32.R12, a32.SP, 60)
	armContextArg(&a)
	a.MovReg(a32.R11, a32.R0)
	a.MovImm32(a32.R0, 42)
	call := a.Call()
	armExit(&a)
	a.PatchCall(call, len(a.B))
	runARM32Exit(t, qemu, append(a.B, fn...), 42)
}

func arm32CallModule(t *testing.T, trapping bool) *wasm.Module {
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

func arm32RecursiveModule(t *testing.T) *wasm.Module {
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
	qemu, err := exec.LookPath("qemu-arm")
	if err != nil {
		t.Skip("qemu-arm not installed")
	}
	t.Run("recursion", func(t *testing.T) {
		cm, err := CompileModule(arm32RecursiveModule(t))
		if err != nil {
			t.Fatal(err)
		}
		var a a32.Asm
		armMemoryContext(&a)
		armContextArg(&a)
		a.MovReg(a32.R11, a32.R0)
		call := a.Call()
		armExit(&a)
		a.PatchCall(call, len(a.B)+cm.Entry[1])
		runARM32Exit(t, qemu, append(a.B, cm.Code...), 1)
	})
	for _, trapping := range []bool{false, true} {
		name := "live-value"
		if trapping {
			name = "nested-trap"
		}
		t.Run(name, func(t *testing.T) {
			cm, err := CompileModule(arm32CallModule(t, trapping))
			if err != nil {
				t.Fatal(err)
			}
			var a a32.Asm
			armMemoryContext(&a)
			armContextArg(&a)
			a.MovReg(a32.R11, a32.R0)
			call := a.Call()
			if trapping {
				a.Ldr(a32.R0, a32.SP, 32)
			}
			armExit(&a)
			a.PatchCall(call, len(a.B)+cm.Entry[1])
			want := 42
			if trapping {
				want = int(embedded32.TrapUnreachable)
			}
			runARM32Exit(t, qemu, append(a.B, cm.Code...), want)
		})
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

func arm32UnreachableModule(t *testing.T) *wasm.Module {
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

func arm32GrowModule(t *testing.T) *wasm.Module {
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
	t.Run("unreachable", func(t *testing.T) {
		unreachable, err := CompileModule(arm32UnreachableModule(t))
		if err != nil {
			t.Fatal(err)
		}
		meta := unreachable.Functions[0]
		unreachableFn := unreachable.Code[meta.Offset : meta.Offset+meta.Size]
		var a a32.Asm
		armMemoryContext(&a)
		armContextArg(&a)
		a.MovReg(a32.R11, a32.R0)
		call := a.Call()
		a.Ldr(a32.R0, a32.SP, 32)
		armExit(&a)
		a.PatchCall(call, len(a.B))
		runARM32Exit(t, qemu, append(a.B, unreachableFn...), int(embedded32.TrapUnreachable))
	})
	t.Run("stack-overflow", func(t *testing.T) {
		var a a32.Asm
		armMemoryContext(&a)
		a.Str(a32.SP, a32.SP, 40)
		armContextArg(&a)
		a.MovReg(a32.R11, a32.R0)
		a.MovImm32(a32.R0, 4)
		call := a.Call()
		a.Ldr(a32.R0, a32.SP, 32)
		armExit(&a)
		a.PatchCall(call, len(a.B))
		runARM32Exit(t, qemu, append(a.B, fn...), int(embedded32.TrapStackOverflow))
	})
	t.Run("canceled-entry", func(t *testing.T) {
		var a a32.Asm
		armMemoryContext(&a)
		a.MovImm32(a32.R12, 1)
		a.Str(a32.R12, a32.SP, 56)
		armContextArg(&a)
		a.MovReg(a32.R11, a32.R0)
		a.MovImm32(a32.R0, 4)
		call := a.Call()
		a.Ldr(a32.R0, a32.SP, 32)
		armExit(&a)
		a.PatchCall(call, len(a.B))
		runARM32Exit(t, qemu, append(a.B, fn...), int(embedded32.TrapCanceled))
	})
	t.Run("memory-grow-failure", func(t *testing.T) {
		grow, err := CompileModule(arm32GrowModule(t))
		if err != nil {
			t.Fatal(err)
		}
		meta := grow.Functions[0]
		growFn := grow.Code[meta.Offset : meta.Offset+meta.Size]
		var a a32.Asm
		armMemoryContext(&a)
		a.MovImm32(a32.R12, 0)
		a.Str(a32.R12, a32.SP, 20)
		a.Str(a32.R12, a32.SP, 36)
		armContextArg(&a)
		a.MovReg(a32.R11, a32.R0)
		a.MovImm32(a32.R0, 1)
		call := a.Call()
		armExit(&a)
		a.PatchCall(call, len(a.B))
		runARM32Exit(t, qemu, append(a.B, growFn...), 255)
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
	armMemoryContext(&wrapper)
	armContextArg(&wrapper)
	wrapper.MovReg(a32.R11, a32.R0)
	wrapper.MovImm32(a32.R0, 41)
	call := wrapper.Call()
	armExit(&wrapper)
	if !wrapper.PatchCall(call, len(wrapper.B)) {
		t.Fatal("wrapper call patch rejected")
	}
	runARM32Exit(t, qemu, append(wrapper.B, fn...), 42)
}
