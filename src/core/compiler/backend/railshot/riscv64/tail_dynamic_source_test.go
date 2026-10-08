//go:build linux && !tinygo

package riscv64

import (
	"encoding/binary"
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func dynamicTailTestSource(params, results []wasm.ValType, bodies ...[]byte) []byte {
	indices := wasmtest.ULEB(uint32(len(bodies)))
	var codes [][]byte
	for _, body := range bodies {
		indices = append(indices, 0)
		body = append([]byte{0}, body...)
		codes = append(codes, append(wasmtest.ULEB(uint32(len(body))), body...))
	}
	// Type2 duplicates the call type0, while type1 is the result-only block
	// signature. The declared call type differs from import0 and definition1.
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, results), wasmtest.FuncType(nil, results), wasmtest.FuncType(params, results))), wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("peer"), wasmtest.Name("run")...), 0, 0))), wasmtest.Section(3, indices), wasmtest.Section(4, []byte{1, 0x70, 1, 2, 2}), wasmtest.Section(9, []byte{2, 0, 0x41, 0, 0x0b, 1, 0, 3, 0, 1, 1}), wasmtest.Section(10, wasmtest.Vec(codes...)))
}

func dynamicTailCountdownBody(payload int) []byte {
	b := []byte{0x20, 0, 0x50, 0x04, 1}
	for n := 0; n < payload; n++ {
		b = append(b, 0x20, byte(n+1))
	}
	b = append(b, 0x05, 0x20, 0, 0x42, 1, 0x7d)
	for n := 0; n < payload; n++ {
		b = append(b, 0x20, byte(n+1))
	}
	return append(b, 0x41, 0, 0x13, 2, 0, 0x0b, 0x0b)
}

func TestTailDynamicSourceExactTypeAndFullWidthPlan(t *testing.T) {
	for _, force := range []bool{false, true} {
		params := []wasm.ValType{wasm.I64, wasm.I32, wasm.F32, wasm.I64, wasm.F64}
		if force {
			params = append(params, wasm.V128)
		}
		source := dynamicTailTestSource(params, params[1:], dynamicTailCountdownBody(len(params)-1))
		i, err := compileDynamicTailSource(source, 7, force)
		if err != nil {
			t.Fatal(force, err)
		}
		if len(i.plans) != 1 || len(i.types) != 3 {
			t.Fatal("symbolic source shape")
		}
		p := i.plans[0]
		frame := i.functions[0].FrameBytes
		stride := 8
		if force {
			stride = 16
		}
		if p.kind != 1 || p.typeIndex != 2 || p.functionIndex != 1 || p.site != 7 || p.sourceStride != stride || p.referenceBase < p.sourceBase+len(params)*stride || p.referenceBase > frame-8 || p.referenceBase&7 != 0 {
			t.Fatal("declared type/reference home", p, frame)
		}
		if !wasm.FuncTypeEqual(i.types[p.typeIndex], i.signatures[1]) || wasm.FuncTypeEqual(i.types[1], i.signatures[1]) {
			t.Fatal("declared type namespace lost")
		}
		// A full-width reference spill is emitted before the capability call;
		// the final tail transfer uses JALR x0, not an ordinary imported bridge.
		wide, nonlink := false, false
		for off := 0; off+4 <= len(i.code); off += 4 {
			w := binary.LittleEndian.Uint32(i.code[off:])
			if w&0x01fff07f == 0x00e13023 {
				wide = true
			}
			if w == 0x000e0067 {
				nonlink = true
			}
		}
		if !wide || !nonlink {
			t.Fatal("missing wide reference/nonlink transfer", force, wide, nonlink)
		}
		// Existing public qualification options must still reject this shape.
		m, err := wasm.DecodeModule(source)
		if err != nil {
			t.Fatal(err)
		}
		if image, err := CompileModuleWith(m, localTailOptions()); image != nil || !errors.Is(err, ErrUnsupported) {
			t.Fatal("public dynamic source gate widened", force, err)
		}
		opts := localTailOptions()
		opts.dynamicTailCalls = true
		if image, err := CompileModuleWith(m, opts); image != nil || !errors.Is(err, ErrUnsupported) {
			t.Fatal("explicit private dynamic public option", force, err)
		}
		if image, err := stageProductionModule(&Compiled{code: i.code}, CompileOptions{NativeTailCalls: true, tailPlans: &i.plans}); image != nil || !errors.Is(err, ErrUnsupported) {
			t.Fatal("non-direct plan reached public staging", force, err)
		}
	}
}

func TestTailDynamicSourceRejectsUnsupportedModuleState(t *testing.T) {
	// These are readable, decoded Wasm modules. Reject the whole shape before
	// any image is published, even when no supported tail site is reachable.
	for _, tc := range []struct {
		name   string
		source []byte
	}{
		{"state-import", wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))), wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("peer"), wasmtest.Name("memory")...), 2, 0, 0))), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(4, []byte{1, 0x70, 0, 0}), wasmtest.Section(10, []byte{1, 2, 0, 0x0b}))},
		{"start", wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(4, []byte{1, 0x70, 0, 0}), wasmtest.Section(8, []byte{0}), wasmtest.Section(10, []byte{1, 2, 0, 0x0b}))},
		{"memory", wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(4, []byte{1, 0x70, 0, 0}), wasmtest.Section(5, []byte{1, 0, 0}), wasmtest.Section(10, []byte{1, 2, 0, 0x0b}))},
		{"global", wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(4, []byte{1, 0x70, 0, 0}), wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, false, []byte{0x41, 0, 0x0b}))), wasmtest.Section(10, []byte{1, 2, 0, 0x0b}))},
		{"reference-signature", wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.FuncRef}, []wasm.ValType{wasm.FuncRef}))), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(4, []byte{1, 0x70, 0, 0}), wasmtest.Section(10, []byte{1, 4, 0, 0x20, 0, 0x0b}))},
		{"table64", wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(4, []byte{1, 0x70, 4, 0}), wasmtest.Section(10, []byte{1, 2, 0, 0x0b}))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := wasm.DecodeModule(tc.source); err != nil {
				t.Fatal("unreadable rejection fixture", err)
			}
			for _, force := range []bool{false, true} {
				if image, err := compileDynamicTailSource(tc.source, 0, force); image != nil || !errors.Is(err, ErrUnsupported) {
					t.Fatal("unsupported module shape admitted", force, err)
				}
			}
		})
	}
}

func TestTailDynamicSourceWholeDeadBudgetAndClosedShapes(t *testing.T) {
	for _, force := range []bool{false, true} {
		for _, count := range []int{1024, 1025} {
			body := []byte{0}
			for n := 0; n < count; n++ {
				body = append(body, 0x13, 2, 0)
			}
			body = append(body, 0x0b)
			i, err := compileDynamicTailSource(dynamicTailTestSource(nil, nil, body), 0, force)
			if count == 1024 {
				if err != nil || len(i.plans) != 0 {
					t.Fatal("dead bounded source", force, err)
				}
			} else if i != nil || !errors.Is(err, ErrUnsupported) {
				t.Fatal("dead source budget emitted", force, err)
			}
		}
	}
	valid := dynamicTailTestSource(nil, nil, []byte{0x41, 0, 0x13, 2, 0, 0x0b})
	if i, err := compileDynamicTailSource(valid, tailRAMaxSites, false); i != nil || !errors.Is(err, ErrUnsupported) {
		t.Fatal("flat site bound", err)
	}
	// A hidden unsupported instruction prevents every visible function image.
	if i, err := compileDynamicTailSource(dynamicTailTestSource(nil, nil, []byte{0x0b}, []byte{0, 0x15, 2, 0x0b}), 0, false); i != nil || err == nil {
		t.Fatal("hidden unsupported reference tail staged", err)
	}
}
