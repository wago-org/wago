package wasm

import "testing"

func checkDeferredDecodeAgreement(t *testing.T, data []byte) {
	t.Helper()
	features := ValidationFeatures{MultiMemory: true}
	var wantAnalysis, gotAnalysis ValidatedModuleAnalysis
	want, wantErr := DecodeModuleWithFeatures(data, features)
	if wantErr == nil {
		wantErr = ValidateModuleWithAnalysis(want, features, 1, ValidationLimits{}, &wantAnalysis)
	}
	got, gotErr := DecodeModuleForValidation(data, features)
	if gotErr == nil {
		gotErr = ValidateModuleWithAnalysis(got, features, 1, ValidationLimits{}, &gotAnalysis)
	}
	if (wantErr == nil) != (gotErr == nil) {
		t.Fatalf("ordinary decode+validate=%v, deferred decode+validate=%v", wantErr, gotErr)
	}
	if wantErr == nil {
		if gotAnalysis.Flags() != wantAnalysis.Flags() || gotAnalysis.FuncCount() != wantAnalysis.FuncCount() {
			t.Fatal("deferred decoding changed validated module facts")
		}
		for i := 0; i < wantAnalysis.FuncCount(); i++ {
			if gotAnalysis.Func(i) != wantAnalysis.Func(i) {
				t.Fatalf("deferred decoding changed function %d facts", i)
			}
		}
	}
}

func TestDeferredFunctionDecodeRejectsSyntaxAndDataCountEdges(t *testing.T) {
	for _, body := range [][]byte{
		{0x00},                         // local declarations, missing expression
		{0x00, 0x01},                   // missing end
		{0x00, 0x0b, 0x01},             // trailing instruction
		{0x00, 0x05, 0x0b},             // unmatched else
		{0x00, 0x02, 0x40, 0x05, 0x0b}, // else inside block
		{0x00, 0x20, 0x80},             // truncated index
		{0x00, 0x41, 0x80},             // truncated constant
		{0x00, 0xfc, 0x09, 0x00, 0x0b}, // data.drop without data count
	} {
		data := localRunsModule(nil, body)
		checkDeferredDecodeAgreement(t, data)
		m, err := DecodeModuleForValidation(data, ValidationFeatures{MultiMemory: true})
		if err == nil {
			err = ValidateModule(m)
		}
		if err == nil {
			t.Fatalf("accepted malformed function body %x", body)
		}
	}
}

func FuzzDeferredDecodeValidationAgreement(f *testing.F) {
	for kind := uint8(0); kind < 7; kind++ {
		for mutation := uint8(0); mutation < 5; mutation++ {
			f.Add(kind, uint8(8), mutation, uint32(17))
		}
	}
	f.Fuzz(func(t *testing.T, kind, funcs, mutation uint8, arg uint32) {
		data := mutateDifferentialModule(generatedDifferentialModule(kind, funcs), mutation, arg)
		checkDeferredDecodeAgreement(t, data)
	})
}
