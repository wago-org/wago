package wasm

import (
	"errors"
	"testing"
)

func TestValidateModuleWithWorkersParity(t *testing.T) {
	data := benchmarkDecodeValidateModule(256)
	m, err := DecodeModule(data)
	if err != nil {
		t.Fatalf("DecodeModule: %v", err)
	}
	if err := ValidateModule(m); err != nil {
		t.Fatalf("serial validation: %v", err)
	}
	for _, workers := range []int{0, 1, 2, 4, 8, 512} {
		for run := 0; run < 10; run++ {
			if err := ValidateModuleWithWorkers(m, workers); err != nil {
				t.Fatalf("workers=%d run=%d: %v", workers, run, err)
			}
		}
	}
}

func TestValidateModuleWithWorkersLowestFunctionError(t *testing.T) {
	const funcs = 16
	m := &Module{
		Types:     []RecType{ft(nil, nil)},
		FuncTypes: make([]TypeIdx, funcs),
		Code:      make([]Func, funcs),
	}
	m.Code[3].Body.Instrs = []Instruction{{Kind: InstrLocalGet, Index: 0}}
	m.Code[11].Body.Instrs = []Instruction{{Kind: InstrCall, Index: 99}}

	want := ValidateModule(m)
	var wantValidation *ValidationError
	if !errors.As(want, &wantValidation) {
		t.Fatalf("serial error = %T %v, want ValidationError", want, want)
	}
	if wantValidation.Func != 3 || wantValidation.Code != ErrUnknownLocal {
		t.Fatalf("serial error = %+v, want function 3 unknown local", wantValidation)
	}

	for _, workers := range []int{2, 4, 8, 32} {
		for run := 0; run < 100; run++ {
			got := ValidateModuleWithWorkers(m, workers)
			var gotValidation *ValidationError
			if !errors.As(got, &gotValidation) {
				t.Fatalf("workers=%d run=%d error = %T %v, want ValidationError", workers, run, got, got)
			}
			if *gotValidation != *wantValidation {
				t.Fatalf("workers=%d run=%d error = %+v, want %+v", workers, run, gotValidation, wantValidation)
			}
		}
	}
}

func TestValidateModuleWithWorkersInvalidTypeCacheMiss(t *testing.T) {
	m, err := DecodeModule(benchmarkDecodeValidateModule(64))
	if err != nil {
		t.Fatalf("DecodeModule: %v", err)
	}
	// call_indirect type 127 misses the frozen valid-type cache. Every fourth
	// function uses it so several workers can hit the malformed index together.
	for i := 0; i < len(m.Code); i += 4 {
		m.Code[i].BodyBytes = []byte{0x11, 0x7f, 0x00, 0x0b}
	}
	want := ValidateModule(m)
	if want == nil {
		t.Fatal("serial validation unexpectedly accepted invalid type index")
	}
	for run := 0; run < 100; run++ {
		got := ValidateModuleWithWorkers(m, 8)
		if got == nil || got.Error() != want.Error() {
			t.Fatalf("run=%d parallel error = %v, want %v", run, got, want)
		}
	}
}

func TestValidateDecodedByteBackedModuleWithWorkers(t *testing.T) {
	dm, err := DecodeModuleByteBacked(benchmarkDecodeValidateModule(256))
	if err != nil {
		t.Fatalf("DecodeModuleByteBacked: %v", err)
	}
	for _, workers := range []int{1, 2, 4, 8} {
		if err := ValidateDecodedByteBackedModuleWithWorkers(dm, workers); err != nil {
			t.Fatalf("workers=%d: %v", workers, err)
		}
	}
}
