package wasm_test

import (
	"reflect"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func validationAnalysisModule(t *testing.T) *wasm.Module {
	t.Helper()
	body := []byte{
		0x03, 0x40, // loop
		0x41, 0x00, // i32.const 0
		0x0d, 0x00, // br_if 0
		0x0b,       // end loop
		0x10, 0x00, // call function 0
		0x0b, // end function
	}
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body), wasmtest.Code(body))),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return m
}

func TestValidatedFuncFactsSize(t *testing.T) {
	if got, want := unsafe.Sizeof(wasm.ValidatedFuncFacts{}), uintptr(8); got != want {
		t.Fatalf("ValidatedFuncFacts size = %d, want %d", got, want)
	}
}

func TestValidationAnalysisRefusesTreeAndMixedBodies(t *testing.T) {
	for _, treeCount := range []int{0, 1, 2} {
		for _, workers := range []int{1, 2} {
			m := validationAnalysisModule(t)
			for i := range m.Code {
				m.Code[i].BodyBytes = []byte{0x10, 0, 0x0b}
			}
			for i := 0; i < treeCount; i++ {
				m.Code[i].BodyBytes = nil
				m.Code[i].Body = wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrCall, Index: 0}}}
			}
			var analysis wasm.ValidatedModuleAnalysis
			if err := wasm.ValidateModuleWithAnalysis(m, wasm.ValidationFeatures{}, workers, wasm.ValidationLimits{}, &analysis); err != nil {
				t.Fatal(err)
			}
			if got, want := analysis.ValidFor(m), treeCount == 0; got != want {
				t.Errorf("%d tree bodies, %d workers: ValidFor = %v, want %v", treeCount, workers, got, want)
			}
			if treeCount == 0 {
				facts := analysis.Func(0)
				facts.Flags = 0
				if analysis.Func(0).Flags == 0 {
					t.Fatal("caller modified private summary storage")
				}
			}
		}
	}
}

func validationAnalysisSegmentModule(t *testing.T) *wasm.Module {
	t.Helper()
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(12, wasmtest.ULEB(2)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0xfc, 0x09, 0x00, 0x0b}),
			wasmtest.Code([]byte{0xfc, 0x09, 0x01, 0x0b}),
		)),
		wasmtest.Section(11, wasmtest.Vec(
			[]byte{0x01, 0x00},
			[]byte{0x01, 0x00},
		)),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatalf("decode segment module: %v", err)
	}
	return m
}

func TestValidateModuleWithAnalysisReducesSegmentCounts(t *testing.T) {
	m := validationAnalysisSegmentModule(t)
	for _, workers := range []int{1, 2} {
		var analysis wasm.ValidatedModuleAnalysis
		if err := wasm.ValidateModuleWithAnalysis(m, wasm.ValidationFeatures{}, workers, wasm.ValidationLimits{}, &analysis); err != nil {
			t.Fatalf("workers %d: %v", workers, err)
		}
		if analysis.DataStateCount() != 2 || analysis.ElemStateCount() != 0 {
			t.Errorf("workers %d counts = data:%d element:%d, want 2/0", workers, analysis.DataStateCount(), analysis.ElemStateCount())
		}
	}
}

func TestValidateModuleWithAnalysisSerialParallelParity(t *testing.T) {
	m := validationAnalysisModule(t)
	var serial, parallel wasm.ValidatedModuleAnalysis
	if err := wasm.ValidateModuleWithAnalysis(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{}, &serial); err != nil {
		t.Fatalf("serial validation: %v", err)
	}
	if err := wasm.ValidateModuleWithAnalysis(m, wasm.ValidationFeatures{}, 2, wasm.ValidationLimits{}, &parallel); err != nil {
		t.Fatalf("parallel validation: %v", err)
	}
	if !reflect.DeepEqual(serial, parallel) {
		t.Fatalf("parallel analysis differs:\nserial:  %#v\nparallel: %#v", serial, parallel)
	}
	if !serial.ValidFor(m) || !parallel.ValidFor(m) {
		t.Fatal("successful analyses do not identify their validated module")
	}
	if serial.ValidFor(validationAnalysisModule(t)) {
		t.Fatal("analysis accepted a different module with the same shape")
	}
	if serial.FuncCount() != 2 {
		t.Fatalf("function facts = %d, want 2", serial.FuncCount())
	}
	wantFlags := wasm.ValidatedFuncHasControl | wasm.ValidatedFuncHasLoop | wasm.ValidatedFuncHasDirectCall | wasm.ValidatedFuncMayCollect
	for i := 0; i < serial.FuncCount(); i++ {
		facts := serial.Func(i)
		if facts.Flags != wantFlags {
			t.Errorf("function %d flags = %#x, want %#x", i, facts.Flags, wantFlags)
		}
	}
	if serial.Flags() != wantFlags {
		t.Fatalf("module analysis = %#v, want flags %#x", serial, wantFlags)
	}
}

func TestValidateModuleWithAnalysisClearsFailure(t *testing.T) {
	m := validationAnalysisModule(t)
	m.Code[0].BodyBytes = []byte{0xff}
	var analysis wasm.ValidatedModuleAnalysis
	if err := wasm.ValidateModuleWithAnalysis(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{}, &analysis); err == nil {
		t.Fatal("invalid body validated")
	}
	if !reflect.DeepEqual(analysis, wasm.ValidatedModuleAnalysis{}) {
		t.Fatalf("analysis retained partial facts after failure: %#v", analysis)
	}
}
