//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"fmt"
	rv "github.com/wago-org/wago/codegen/riscv64"
	railcore "github.com/wago-org/wago/src/core/compiler/backend/railshot"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/plugins"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"strings"
	"testing"
)

func widePluginTestModule(carrier wasm.ValType, body []byte) *wasm.Module {
	wire := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{carrier}), wasmtest.FuncType([]wasm.ValType{carrier}, nil), wasmtest.FuncType(nil, nil))), wasmtest.Section(2, wasmtest.Vec(widePluginTestImport("custom", "new", 0), widePluginTestImport("custom", "drop", 1))), wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 2))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
	m, err := wasm.DecodeModuleByteBacked(wire)
	if err != nil {
		panic(err)
	}
	return m.Module
}
func widePluginTestInstructions(custom plugins.CustomType) map[uint32]railcore.CustomInstruction {
	return map[uint32]railcore.CustomInstruction{
		0: {CustomOutput: &custom, ResultWidth: custom.Bits(), Codegen: &rv.Lowering{Compatibility: rv.CompatibilityFullAccess, Emit: func(ctx rv.Context) error {
			if custom.Size() > 128 {
				for i := 0; i < int(custom.Size()/8); i++ {
					r := ctx.AllocGP()
					ctx.Encoder().Constant64(r, int64(i+1))
					if err := ctx.OutputCustomCell(i, r); err != nil {
						return err
					}
					ctx.ReleaseGP(r)
				}
				return nil
			}
			cells := make([]rv.Reg, custom.Size()/8)
			for i := range cells {
				cells[i] = ctx.AllocGP()
				ctx.Encoder().Constant64(cells[i], int64(i+1))
			}
			return ctx.OutputCustom(cells...)
		}}},
		1: {CustomInputs: []plugins.CustomType{custom}, InputWidths: []int32{custom.Bits()}, Codegen: &rv.Lowering{Compatibility: rv.CompatibilityManaged, Managed: func(ctx rv.ManagedContext) error {
			if custom.Size() > 128 {
				for i := 0; i < int(custom.Size()/8); i++ {
					r, err := ctx.InputCustomCell(0, i)
					if err != nil {
						return err
					}
					ctx.ReleaseGP(r)
				}
				return nil
			}
			cells, err := ctx.InputCustom(0)
			if err != nil {
				return err
			}
			for _, cell := range cells {
				ctx.ReleaseGP(cell)
			}
			return nil
		}}},
	}
}
func TestPluginRV64WideCarrierAdmission(t *testing.T) {
	for _, carrier := range []plugins.WasmType{plugins.WasmI32, plugins.WasmI64, plugins.WasmF32, plugins.WasmF64, plugins.WasmV128, plugins.WasmFuncRef, plugins.WasmExternRef} {
		t.Run(fmt.Sprint(carrier), func(t *testing.T) {
			custom, err := plugins.PrepareCustomType(plugins.CustomTypeSpec{Name: "wide", Size: 32, Carrier: carrier})
			if err != nil {
				t.Fatal(err)
			}
			opts := CompileOptions{ImportBindings: []ImportBinding{{Dynamic: true, ImportIndex: 0}, {Dynamic: true, ImportIndex: 1}}, SyncHostCalls: true, SyncHostSlots: 2, Interruptible: true, InvocationContext: true, ForeignTableCalls: true, CustomInstructions: widePluginTestInstructions(custom)}
			_, err = CompileModuleWith(widePluginTestModule(pluginCarrierType(custom), []byte{0x10, 0, 0x10, 1, 0x0b}), opts)
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestPluginRV64WideTypeIdentity(t *testing.T) {
	a, _ := plugins.PrepareCustomType(plugins.CustomTypeSpec{Name: "a", Size: 32, Carrier: plugins.WasmI32})
	b, _ := plugins.PrepareCustomType(plugins.CustomTypeSpec{Name: "b", Size: 32, Carrier: plugins.WasmI32})
	instructions := widePluginTestInstructions(a)
	consumer := instructions[1]
	consumer.CustomInputs = []plugins.CustomType{b}
	instructions[1] = consumer
	_, err := CompileModuleWith(widePluginTestModule(wasm.I32, []byte{0x10, 0, 0x10, 1, 0x0b}), CompileOptions{ImportBindings: []ImportBinding{{Dynamic: true, ImportIndex: 0}, {Dynamic: true, ImportIndex: 1}}, SyncHostCalls: true, SyncHostSlots: 2, Interruptible: true, InvocationContext: true, ForeignTableCalls: true, CustomInstructions: instructions})
	if err == nil || !strings.Contains(err.Error(), "incompatible custom type") {
		t.Fatalf("identity error=%v", err)
	}
}

func TestPluginRV64CustomCarrierDoesNotHideDefinedReferenceABI(t *testing.T) {
	custom, _ := plugins.PrepareCustomType(plugins.CustomTypeSpec{Name: "reference-erasure", Size: 32, Carrier: plugins.WasmFuncRef})
	m := widePluginTestModule(wasm.FuncRef, []byte{0x10, 0, 0x10, 1, 0x0b})
	// The ordinary definition shares the custom producer's physical type. It
	// returns a real null reference and has no logical custom annotation.
	index := m.FuncTypes[0]
	index.Index = 0
	m.FuncTypes = append(m.FuncTypes, index)
	m.Code = append(m.Code, wasm.Func{BodyBytes: []byte{0xd0, 0x70, 0x0b}})
	references, err := hasDefinedReferenceFunctions(m, pluginInstructions(widePluginTestInstructions(custom)))
	if err != nil || !references {
		t.Fatalf("ordinary shared reference signature lost its ABI: references=%t err=%v", references, err)
	}
}

func widePluginTestImport(module, name string, index byte) []byte {
	b := append(wasmtest.ULEB(uint32(len(module))), []byte(module)...)
	b = append(b, wasmtest.ULEB(uint32(len(name)))...)
	b = append(b, []byte(name)...)
	return append(b, 0, index)
}

func TestPluginRV64WideControlAdmission(t *testing.T) {
	for _, carrier := range []plugins.WasmType{plugins.WasmI32, plugins.WasmI64, plugins.WasmF32, plugins.WasmF64, plugins.WasmV128, plugins.WasmFuncRef, plugins.WasmExternRef} {
		custom, _ := plugins.PrepareCustomType(plugins.CustomTypeSpec{Name: "wide", Size: 32, Carrier: carrier})
		physical := pluginCarrierType(custom)
		wire := byte(0x7f)
		switch carrier {
		case plugins.WasmI64:
			wire = 0x7e
		case plugins.WasmF32:
			wire = 0x7d
		case plugins.WasmF64:
			wire = 0x7c
		case plugins.WasmV128:
			wire = 0x7b
		case plugins.WasmFuncRef:
			wire = 0x70
		case plugins.WasmExternRef:
			wire = 0x6f
		}
		bodies := map[string][]byte{
			"branch": {0x02, wire, 0x10, 0, 0x10, 0, 0x0c, 0, 0x0b, 0x10, 1, 0x0b},
			"join":   {0x41, 1, 0x04, wire, 0x10, 0, 0x05, 0x10, 0, 0x0b, 0x10, 1, 0x0b},
			"select": {0x10, 0, 0x10, 0, 0x41, 0, 0x1c, 1, wire, 0x10, 1, 0x0b},
			"spills": {0x10, 0, 0x10, 0, 0x10, 0, 0x10, 0, 0x10, 1, 0x10, 1, 0x10, 1, 0x10, 1, 0x0b},
		}
		for name, body := range bodies {
			t.Run(fmt.Sprintf("%d/%s", carrier, name), func(t *testing.T) {
				_, err := CompileModuleWith(widePluginTestModule(physical, body), CompileOptions{ImportBindings: []ImportBinding{{Dynamic: true, ImportIndex: 0}, {Dynamic: true, ImportIndex: 1}}, SyncHostCalls: true, SyncHostSlots: 2, Interruptible: true, InvocationContext: true, ForeignTableCalls: true, CustomInstructions: widePluginTestInstructions(custom)})
				if err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestPluginRV64WideRejectsRealReference(t *testing.T) {
	custom, _ := plugins.PrepareCustomType(plugins.CustomTypeSpec{Name: "wide", Size: 32, Carrier: plugins.WasmFuncRef})
	_, err := CompileModuleWith(widePluginTestModule(wasm.FuncRef, []byte{0xd0, 0x70, 0x10, 1, 0x0b}), CompileOptions{ImportBindings: []ImportBinding{{Dynamic: true, ImportIndex: 0}, {Dynamic: true, ImportIndex: 1}}, SyncHostCalls: true, SyncHostSlots: 2, Interruptible: true, InvocationContext: true, ForeignTableCalls: true, CustomInstructions: widePluginTestInstructions(custom)})
	if err == nil || !strings.Contains(err.Error(), "incompatible custom type") {
		t.Fatalf("real reference error=%v", err)
	}
}
func TestPluginRV64WideRejectsMixedJoins(t *testing.T) {
	custom, _ := plugins.PrepareCustomType(plugins.CustomTypeSpec{Name: "wide", Size: 32, Carrier: plugins.WasmI32})
	for _, body := range [][]byte{
		{0x41, 1, 0x04, 0x7f, 0x10, 0, 0x05, 0x41, 0, 0x0b, 0x10, 1, 0x0b},
		{0x41, 1, 0x04, 0x7f, 0x41, 0, 0x05, 0x10, 0, 0x0b, 0x10, 1, 0x0b},
		{0x10, 0, 0x41, 0, 0x41, 0, 0x1b, 0x10, 1, 0x0b},
	} {
		_, err := CompileModuleWith(widePluginTestModule(wasm.I32, body), CompileOptions{ImportBindings: []ImportBinding{{Dynamic: true, ImportIndex: 0}, {Dynamic: true, ImportIndex: 1}}, SyncHostCalls: true, SyncHostSlots: 2, Interruptible: true, InvocationContext: true, ForeignTableCalls: true, CustomInstructions: widePluginTestInstructions(custom)})
		if err == nil {
			t.Fatal("custom value mixed with an ordinary carrier", body)
		}
	}
}

func TestPluginRV64WideFrameFootprint(t *testing.T) {
	for _, size := range []int32{16, 32, 64, 128, 256, 512} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			custom, _ := plugins.PrepareCustomType(plugins.CustomTypeSpec{Name: "wide", Size: size, Carrier: plugins.WasmI32})
			mode := emissionMode{production: true, interruptible: true, invocationContext: true, foreignTableCalls: true, customInstructions: pluginInstructions(widePluginTestInstructions(custom))}
			compile := func(body []byte) FunctionInfo {
				m := widePluginTestModule(wasm.I32, body)
				types := make([]*wasm.CompType, len(m.Types))
				for i := range types {
					types[i] = &m.Types[i].SubTypes[0].Comp
				}
				functions := make([]*wasm.CompType, 3)
				for i := range functions {
					functions[i], _ = m.FuncSignature(uint32(i))
				}
				code, info, _, err := compileSWARPairsWithImports(&m.Code[0], functions[2], nil, types, functions, nil, 0, nil, nil, 0, 2, mode)
				if err != nil {
					t.Fatal(err)
				}
				info.CodeBytes = len(code)
				return info
			}
			used := compile([]byte{0x10, 0, 0x10, 1, 0x0b})
			unused := compile([]byte{0x41, 1, 0x1a, 0x0b})
			t.Logf("size=%d used-frame-bytes=%d unused-frame-bytes=%d body-code-bytes=%d", size, used.FrameBytes, unused.FrameBytes, used.CodeBytes)
			if used.FrameBytes-unused.FrameBytes != int(size)-16 {
				t.Fatal("wide home adds more than its required payload bytes", used.FrameBytes, unused.FrameBytes)
			}
		})
	}
}

func TestPluginRV64CustomCellBoundsAndLifetime(t *testing.T) {
	custom, _ := plugins.PrepareCustomType(plugins.CustomTypeSpec{Name: "cells", Size: 32, Carrier: plugins.WasmI32})
	c := &pluginRV64Context{f: &integerEmitter{}, active: true, inputs: []int32{64}, outputOffset: 64, instruction: railcore.CustomInstruction{CustomInputs: []plugins.CustomType{custom}, CustomOutput: &custom}}
	for _, cell := range []int{-1, 4} {
		if _, err := c.InputCustomCell(0, cell); err == nil {
			t.Fatal("invalid input cell admitted", cell)
		}
	}
	r := c.AllocGP()
	if c.OutputCustomCell(-1, r) == nil || c.OutputCustomCell(4, r) == nil || c.OutputCustomCell(0, rv.Zero) == nil {
		t.Fatal("invalid output cell admitted")
	}
	if err := c.OutputCustomCell(0, r); err != nil {
		t.Fatal(err)
	}
	if c.OutputCustomCell(0, r) == nil {
		t.Fatal("duplicate output cell admitted")
	}
	if _, err := c.InputCustomCell(0, 0); err == nil {
		t.Fatal("overwritten input read")
	}
	if _, err := c.InputCustom(0); err == nil {
		t.Fatal("overwritten bundle read")
	}
	if _, err := c.OutputCustomAddress(); err == nil {
		t.Fatal("mixed output methods admitted")
	}
	c.active = false
	if _, err := c.InputCustomCell(0, 1); err == nil {
		t.Fatal("stale cell input context")
	}
	if _, err := c.InputCustomAddress(0); err == nil {
		t.Fatal("stale address input context")
	}
	if c.OutputCustomCell(1, r) == nil {
		t.Fatal("stale cell output context")
	}
}
func TestPluginRV64CustomCellRequiresCompleteOutput(t *testing.T) {
	custom, _ := plugins.PrepareCustomType(plugins.CustomTypeSpec{Name: "cells", Size: 32, Carrier: plugins.WasmI32})
	instruction := railcore.CustomInstruction{CustomOutput: &custom, ResultWidth: custom.Bits(), Codegen: &rv.Lowering{Compatibility: rv.CompatibilityFullAccess, Emit: func(ctx rv.Context) error {
		r := ctx.AllocGP()
		ctx.Encoder().Addi(r, rv.Zero, 1)
		return ctx.OutputCustomCell(0, r)
	}}}
	f := &integerEmitter{target: target{mode: emissionMode{customInstructions: pluginInstructions(map[uint32]railcore.CustomInstruction{0: instruction})}}}
	f.emitPluginCall(0, &wasm.CompType{Kind: wasm.CompFunc, Results: []wasm.ValType{wasm.I32}}, 0, f.SpillOffset)
	if f.err == nil || !strings.Contains(f.err.Error(), "unset cells") {
		t.Fatal("partial custom output accepted", f.err)
	}
}

func TestPluginRV64CustomOutputDoesNotReuseScalarMemoryInput(t *testing.T) {
	custom, _ := plugins.PrepareCustomType(plugins.CustomTypeSpec{Name: "cells", Size: 32, Carrier: plugins.WasmI32})
	c := &pluginRV64Context{f: &integerEmitter{target: target{mode: emissionMode{customMemory: true}}}, active: true, inputs: []int32{64}, outputOffset: 64, instruction: railcore.CustomInstruction{InputWidths: []int32{32}, CustomOutput: &custom}}
	r := c.AllocGP()
	if err := c.OutputCustomCell(0, r); err != nil {
		t.Fatal(err)
	}
	if _, err := c.InputI32(0); err == nil {
		t.Fatal("overwritten scalar input read")
	}
	if _, _, _, err := c.CheckedMemory(0, 0, 4); err == nil || !strings.Contains(err.Error(), "overwritten") {
		t.Fatalf("overwritten scalar memory address: %v", err)
	}
}

func TestPluginRV64WideResourceBoundBeforeEmission(t *testing.T) {
	custom, err := plugins.PrepareCustomType(plugins.CustomTypeSpec{Name: "frame-bound", Size: int32(MaxFrameBytes + 16), Carrier: plugins.WasmI32})
	if err != nil {
		t.Fatal(err)
	}
	instructions := widePluginTestInstructions(custom)
	producer := instructions[0]
	called := false
	producer.Codegen = &rv.Lowering{Compatibility: rv.CompatibilityFullAccess, Emit: func(rv.Context) error {
		called = true
		return fmt.Errorf("oversize callback must not run")
	}}
	instructions[0] = producer
	opts := CompileOptions{ImportBindings: []ImportBinding{{Dynamic: true, ImportIndex: 0}, {Dynamic: true, ImportIndex: 1}}, SyncHostCalls: true, SyncHostSlots: 2, Interruptible: true, InvocationContext: true, ForeignTableCalls: true, CustomInstructions: instructions}
	if _, err := CompileModuleWith(widePluginTestModule(wasm.I32, []byte{0x0b}), opts); err != nil {
		t.Fatalf("unused large custom declaration: %v", err)
	}
	if _, err := CompileModuleWith(widePluginTestModule(wasm.I32, []byte{0x10, 0, 0x10, 1, 0x0b}), opts); err == nil || !strings.Contains(err.Error(), "frame") {
		t.Fatalf("live custom frame bound: %v", err)
	}
	if called {
		t.Fatal("custom output emission ran before the native frame bound")
	}
}

func TestPluginRV64MixedWideFrameFootprint(t *testing.T) {
	for _, scalars := range []int{100, 255} {
		t.Run(fmt.Sprint(scalars), func(t *testing.T) { testPluginRV64MixedWideFrameFootprint(t, scalars) })
	}
}

func TestPluginRV64UnusedCustomDirectoryPreservesCode(t *testing.T) {
	custom, _ := plugins.PrepareCustomType(plugins.CustomTypeSpec{Name: "unused", Size: 512, Carrier: plugins.WasmI32})
	m := widePluginTestModule(wasm.I32, []byte{0x41, 1, 0x41, 2, 0x6a, 0x1a, 0x0b})
	types := make([]*wasm.CompType, len(m.Types))
	for i := range types {
		types[i] = &m.Types[i].SubTypes[0].Comp
	}
	functions := make([]*wasm.CompType, 3)
	for i := range functions {
		functions[i], _ = m.FuncSignature(uint32(i))
	}
	mode := emissionMode{production: true, interruptible: true, invocationContext: true, foreignTableCalls: true}
	plain, plainInfo, _, err := compileSWARPairsWithImports(&m.Code[0], functions[2], nil, types, functions, nil, 0, nil, nil, 0, 2, mode)
	if err != nil {
		t.Fatal(err)
	}
	mode.customInstructions = pluginInstructions(widePluginTestInstructions(custom))
	withCustom, customInfo, _, err := compileSWARPairsWithImports(&m.Code[0], functions[2], nil, types, functions, nil, 0, nil, nil, 0, 2, mode)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(plain, withCustom) || plainInfo.FrameBytes != customInfo.FrameBytes {
		t.Fatal("unused custom directory changes ordinary native code or frame")
	}
}

func testPluginRV64MixedWideFrameFootprint(t *testing.T, scalars int) {
	t.Helper()
	custom, _ := plugins.PrepareCustomType(plugins.CustomTypeSpec{Name: "mixed", Size: 512, Carrier: plugins.WasmI32})
	mode := emissionMode{production: true, interruptible: true, invocationContext: true, foreignTableCalls: true, customInstructions: pluginInstructions(widePluginTestInstructions(custom))}
	body := []byte{0x10, 0}
	for range scalars {
		body = append(body, 0x41, 1)
	}
	for range scalars {
		body = append(body, 0x1a)
	}
	body = append(body, 0x10, 1, 0x0b)
	m := widePluginTestModule(wasm.I32, body)
	types := make([]*wasm.CompType, len(m.Types))
	for i := range types {
		types[i] = &m.Types[i].SubTypes[0].Comp
	}
	functions := make([]*wasm.CompType, 3)
	for i := range functions {
		functions[i], _ = m.FuncSignature(uint32(i))
	}
	compile := func() (FunctionInfo, int) {
		code, info, _, err := compileSWARPairsWithImports(&m.Code[0], functions[2], nil, types, functions, nil, 0, nil, nil, 0, 2, mode)
		if err != nil {
			t.Fatal(err)
		}
		return info, len(code)
	}
	info, codeBytes := compile()
	if info.FrameBytes != callFrameHeader+512+scalars*16 {
		t.Fatalf("mixed custom frame contains unused padding: %d", info.FrameBytes)
	}
	allocs := testing.AllocsPerRun(8, func() { compile() })
	t.Logf("mixed: custom-bytes=512 scalar-homes=%d frame-bytes=%d code-bytes=%d compile-allocations=%g", scalars, info.FrameBytes, codeBytes, allocs)
}
