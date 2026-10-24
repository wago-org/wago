//go:build arm64

package arm64

import (
	"reflect"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func hintStreamTestModule(t testing.TB, defs ...funcDef) *wasm.Module {
	t.Helper()
	var types, funcs, codes [][]byte
	for i, def := range defs {
		types = append(types, wasmtest.FuncType(def.params, def.results))
		funcs = append(funcs, wasmtest.ULEB(uint32(i)))
		codes = append(codes, append(wasmtest.ULEB(uint32(len(def.body))), def.body...))
	}
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(funcs...)),
		wasmtest.Section(4, []byte{1, 0x70, 0, 2}),
		wasmtest.Section(5, []byte{1, 0, 1}),
		wasmtest.Section(6, []byte{1, 0x7f, 1, 0x41, 0, 0x0b}),
		wasmtest.Section(10, wasmtest.Vec(codes...)),
	)
	m, err := wasm.DecodeModuleForValidation(data, wasm.ValidationFeatures{MultiMemory: true})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func assertValidationHintParity(t testing.TB, m *wasm.Module, opts CompileOptions) *wasm.ValidatedModuleAnalysis {
	t.Helper()
	var analysis wasm.ValidatedModuleAnalysis
	var collector *moduleValidationHints
	factory := func(m *wasm.Module) wasm.ValidationHintObserver {
		observer := NewValidationHintObserver(m, opts)
		collector, _ = observer.(*moduleValidationHints)
		return observer
	}
	features := wasm.ValidationFeatures{MultiMemory: true, CompactImports: true, ExtendedConstGlobals: true, GCConstExpr: true}
	if err := wasm.ValidateModuleWithHintObserver(m, features, 1, wasm.ValidationLimits{}, &analysis, factory); err != nil {
		t.Fatal(err)
	}
	if !analysis.ValidFor(m) || collector == nil || analysis.HintObserver() != collector {
		if collector != nil {
			t.Fatalf("stream declined: %v, functions %d/%d", collector.err, collector.functions, len(m.Code))
		}
		t.Fatal("stream observer unavailable")
	}
	hints, sidecar, globals, err := computeModuleHintsWithWorkersResidencyPolicy(m, m.GlobalCount(), m.ImportedFuncCount(), 1, collector.policy, false)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(hints, collector.hints) {
		for i := range hints {
			if !reflect.DeepEqual(hints[i], collector.hints[i]) {
				t.Fatalf("function %d hints differ:\nreference %#v\nstream    %#v", i, hints[i], collector.hints[i])
			}
		}
	}
	if !reflect.DeepEqual(sidecar, collector.sidecar) {
		t.Fatalf("hint sidecars differ:\nreference %#v\nstream    %#v", sidecar, collector.sidecar)
	}
	if !reflect.DeepEqual(globals, collector.globals) {
		t.Fatalf("global scores differ: %v versus %v", globals, collector.globals)
	}
	return &analysis
}

func TestValidationHintStreamMatchesScanner(t *testing.T) {
	vector := []byte{0}
	for range 2 {
		vector = append(vector, simdOp(12)...)
		vector = append(vector, make([]byte, 16)...)
	}
	vector = append(vector, simdOp(0xae)...)
	vector = append(vector, 0x1a, 0x0b)
	interval := []byte{1, 40, 0x7f}
	for range 128 {
		interval = append(interval, 0x20, 0, 0x21, 1)
	}
	interval = append(interval, 0x0b)
	cases := []struct {
		name string
		defs []funcDef
	}{
		{"empty", []funcDef{{body: []byte{0, 0x0b}}}},
		{"addition-run", []funcDef{{body: []byte{0, 0x41, 1, 0x41, 2, 0x41, 3, 0x41, 4, 0x6a, 0x6a, 0x6a, 0x1a, 0x0b}}}},
		{"nested-control", []funcDef{{params: []wasm.ValType{wasm.I32, wasm.I32}, body: []byte{1, 2, 0x7f, 0x41, 1, 0x21, 2, 0x20, 0, 0x04, 0x40, 0x20, 2, 0x1a, 0x05, 0x20, 1, 0x1a, 0x0b, 0x03, 0x40, 0x20, 0, 0x1a, 0x02, 0x40, 0x20, 1, 0x1a, 0x0b, 0x0b, 0x0b}}}},
		{"forward-back-calls", []funcDef{{body: []byte{0, 0x03, 0x40, 0x10, 1, 0x0b, 0x10, 1, 0x0b}}, {body: []byte{0, 0x02, 0x40, 0x10, 0, 0x0b, 0x0b}}}},
		{"memory-globals", []funcDef{{params: []wasm.ValType{wasm.I32}, body: []byte{0, 0x20, 0, 0x28, 2, 0, 0x21, 0, 0x20, 0, 0x2d, 0, 0, 0x1a, 0x41, 0, 0x41, 7, 0x36, 2, 0, 0x3f, 0, 0x1a, 0x41, 0, 0x40, 0, 0x1a, 0x03, 0x40, 0x23, 0, 0x41, 1, 0x6a, 0x24, 0, 0x0b, 0x0b}}}},
		{"indirect-call", []funcDef{{body: []byte{0, 0x41, 0, 0x11, 0, 0, 0x0b}}}},
		{"simd-propagation", []funcDef{{body: vector}, {body: []byte{0, 0x0b}}}},
		{"interval-counts", []funcDef{{body: interval}}},
		{"atomic-fence", []funcDef{{body: []byte{0, 0xfe, 3, 0, 0x0b}}}},
		{"table-grow", []funcDef{{body: []byte{0, 0xd0, 0x70, 0x41, 1, 0xfc, 15, 0, 0x1a, 0x0b}}}},
		{"table-fill", []funcDef{{body: []byte{0, 0x41, 0, 0xd0, 0x70, 0x41, 1, 0xfc, 17, 0, 0x0b}}}},
		{"table-copy", []funcDef{{body: []byte{0, 0x41, 0, 0x41, 0, 0x41, 1, 0xfc, 14, 0, 0, 0x0b}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, opts := range []CompileOptions{{Workers: 1}, {Workers: 1, Optimizations: map[string]bool{"loop-int-const": false, "interval-region-pins": false}}, {Workers: 1, CompactNative: true}} {
				m := hintStreamTestModule(t, tc.defs...)
				if tc.name == "nested-control" {
					m.BranchHints = []wasm.FuncBranchHints{{FuncIndex: 0, Hints: []wasm.BranchHint{{Offset: 9, Likely: false}}}}
				}
				assertValidationHintParity(t, m, opts)
			}
		})
	}
}

func TestValidationHintStreamOwnership(t *testing.T) {
	m := hintStreamTestModule(t, funcDef{body: []byte{0, 0x0b}})
	analysis := assertValidationHintParity(t, m, CompileOptions{Workers: 1})
	collector := analysis.HintObserver().(*moduleValidationHints)
	wrongPolicy := collector.policy
	wrongPolicy.FunctionAlignLog2++
	other := hintStreamTestModule(t, funcDef{body: []byte{0, 0x0b}})
	for _, tc := range []struct {
		module   *wasm.Module
		policy   CodegenPolicy
		workers  int
		detailed bool
	}{{other, collector.policy, 1, false}, {m, wrongPolicy, 1, false}, {m, collector.policy, 2, false}, {m, collector.policy, 1, true}} {
		if _, _, _, ok := takeValidationHints(tc.module, analysis, tc.policy, tc.workers, tc.detailed); ok {
			t.Fatal("incompatible consumer took validation hints")
		}
	}
	if collector.taken != 0 {
		t.Fatal("fallback consumed hints")
	}
	if _, _, _, ok := takeValidationHints(m, analysis, collector.policy, 1, false); !ok {
		t.Fatal("compatible consumer declined hints")
	}
	if _, _, _, ok := takeValidationHints(m, analysis, collector.policy, 1, false); ok {
		t.Fatal("hints consumed twice")
	}
}

func TestValidationHintStreamCompilerConsumesProof(t *testing.T) {
	m := hintStreamTestModule(t, funcDef{body: []byte{0, 0x0b}})
	analysis := assertValidationHintParity(t, m, CompileOptions{Workers: 1})
	if _, err := CompileModuleWith(m, CompileOptions{Workers: 1, ValidationAnalysis: analysis}); err != nil {
		t.Fatal(err)
	}
	if analysis.HintObserver().(*moduleValidationHints).taken != 1 {
		t.Fatal("compiler fell back instead of consuming proof")
	}
}
