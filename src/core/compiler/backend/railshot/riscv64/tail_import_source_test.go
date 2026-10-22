//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func tailImportTestSource(params, results []wasm.ValType, bodies ...[]byte) []byte {
	imports := append(wasmtest.Name("p"), wasmtest.Name("tail")...)
	imports = append(imports, 0, 0) // function, type0
	indices := wasmtest.ULEB(uint32(len(bodies)))
	var codes [][]byte
	for _, body := range bodies {
		indices = append(indices, 0)
		body = append([]byte{0}, body...)
		codes = append(codes, append(wasmtest.ULEB(uint32(len(body))), body...))
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, results), wasmtest.FuncType(nil, results))),
		wasmtest.Section(2, wasmtest.Vec(imports)),
		wasmtest.Section(3, indices),
		wasmtest.Section(10, wasmtest.Vec(codes...)),
	)
}

func TestTailImportSourceScalarSWARSymbolicPlansAndDefaultGate(t *testing.T) {
	source := tailImportTestSource([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, []byte{0x20, 0, 0x12, 0, 0x0b})
	for _, swar := range []bool{false, true} {
		i, err := compileTailImportSource(source, 7, swar)
		if err != nil {
			t.Fatal(swar, err)
		}
		if len(i.plans) != 1 || i.plans[0].site != 7 || i.plans[0].functionIndex != 1 || i.plans[0].importIndex != 0 || i.plans[0].sourceStride != map[bool]int{false: 8, true: 16}[swar] || i.functions[0].FrameHeaderBytes != 48 {
			t.Fatal("symbolic plan/frame", swar, i.plans, i.functions)
		}
		copy, err := compileTailImportSource(bytes.Clone(source), 7, swar)
		if err != nil || !bytes.Equal(copy.code, i.code) {
			t.Fatal("address-independent deterministic source image", err)
		}
	}
	if c, err := CompileModuleWith(localTailDecode(t, source), localTailOptions()); c != nil || !errors.Is(err, ErrUnsupported) {
		t.Fatal("default imported-tail gate opened", err)
	}
}

func TestTailImportSourceDeadSitesAndBeforeEmissionBounds(t *testing.T) {
	dead := []byte{0x00, 0x12, 0, 0x0b}
	live := []byte{0x20, 0, 0x12, 0, 0x0b}
	for _, swar := range []bool{false, true} {
		i, err := compileTailImportSource(tailImportTestSource([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}, dead, live), 23, swar)
		if err != nil || len(i.plans) != 1 || i.plans[0].site != 23 || i.plans[0].functionIndex != 2 {
			t.Fatal("dead tail stole live site ordinal", swar, err, i)
		}
		one := tailImportTestSource([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}, live)
		if i, err := compileTailImportSource(one, 4095, swar); err != nil || len(i.plans) != 1 || i.plans[0].site != 4095 {
			t.Fatal("last site", err)
		}
		if i, err := compileTailImportSource(one, 4096, swar); i != nil || !errors.Is(err, ErrUnsupported) {
			t.Fatal("overflow site admitted", err)
		}
		if i, err := compileTailImportSource(tailImportTestSource([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}, dead, live), 4095, swar); i != nil || !errors.Is(err, ErrUnsupported) {
			t.Fatal("syntactic site budget not charged before emission", err)
		}
		for _, body := range [][]byte{{0, 0x12, 4, 0x0b}, {0, 0x13, 0, 0, 0x0b}, {0, 0x14, 0, 0x0b}} {
			if i, err := compileTailImportSource(tailImportTestSource(nil, nil, body), 0, swar); i != nil || err == nil {
				t.Fatal("hidden unsupported tail admitted", body)
			}
		}
	}
}

func TestTailImportSourceWholeModuleAndExactResults(t *testing.T) {
	valid := tailImportTestSource(nil, nil, []byte{0x12, 0, 0x0b})
	for _, swar := range []bool{false, true} {
		if i, err := compileTailImportSource(valid, 0, swar); err != nil || i.functions[0].FrameHeaderBytes != 48 {
			t.Fatal("zero parameter framed tail", err)
		}
		for _, section := range [][]byte{
			wasmtest.Section(5, wasmtest.Vec([]byte{0, 0})),
			wasmtest.Section(8, []byte{1}),
		} {
			if i, err := compileTailImportSource(append(bytes.Clone(valid), section...), 0, swar); i != nil || err == nil {
				t.Fatal("unsupported state/start admitted", err)
			}
		}
		// A validated hidden function uses an unsupported reference consumer.
		bad := tailImportTestSource(nil, nil, []byte{0x12, 0, 0x0b}, []byte{0xd0, 0x6f, 0x1a, 0x0b})
		localTailDecode(t, bad)
		if i, err := compileTailImportSource(bad, 0, swar); i != nil || err == nil {
			t.Fatal("hidden function escaped whole-module admission", err)
		}
	}
	if tailImportExactSignature(&wasm.CompType{Kind: wasm.CompFunc, Results: []wasm.ValType{wasm.I32}}, &wasm.CompType{Kind: wasm.CompFunc, Results: []wasm.ValType{wasm.F32}}) {
		t.Fatal("equal width is not exact result type")
	}
}

func tailImportPortableFunctions(t *testing.T, i *tailImportImage) []tailRAFunction {
	t.Helper()
	f := tailRAProofFunctionFixture(t, i.signatures[i.imports], tailRAContextBytes)
	f.code = tailRAProofBuffer(t, i.code)
	snapshot := bytes.Clone(i.code)
	var out []tailRAFunction
	for n, info := range i.functions {
		x := f
		x.snapshot = snapshot
		x.info = info
		x.signature = i.signatures[i.imports+n]
		out = append(out, x)
	}
	return out
}

func TestTailImportSourceBindingRejectsCollisionsSignaturesAndWrongImages(t *testing.T) {
	source := tailImportTestSource([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, []byte{0x20, 0, 0x12, 0, 0x0b})
	a, err := compileTailImportSource(source, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	b, err := compileTailImportSource(source, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	af, bf := tailImportPortableFunctions(t, a), tailImportPortableFunctions(t, b)
	s := tailRAOwnerFixture(t, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	s.sites = nil
	bindings := []tailImportBinding{{a, af, []tailRAFunction{bf[0]}}, {b, bf, []tailRAFunction{af[0]}}}
	if out, err := tailImportOwnerSpec(bindings, s); err != nil || len(out.sites) != 2 {
		t.Fatal("contiguous scalar/SWAR bindings", err)
	}
	b.siteBase = 0
	if _, err := tailImportOwnerSpec(bindings, s); !errors.Is(err, ErrUnsupported) {
		t.Fatal("colliding independent module sites", err)
	}
	b.siteBase = 1
	wrong := bf[0]
	wrong.signature = &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.F32}, Results: []wasm.ValType{wasm.I32}}
	bindings[0].targets = []tailRAFunction{wrong}
	if _, err := tailImportOwnerSpec(bindings, s); !errors.Is(err, ErrUnsupported) {
		t.Fatal("same width wrong imported signature", err)
	}
	bindings[0].targets = []tailRAFunction{bf[0]}
	af[0].info.Entry += 16
	if _, err := tailImportOwnerSpec(bindings, s); !errors.Is(err, ErrUnsupported) {
		t.Fatal("wrong frozen source entry", err)
	}
}
