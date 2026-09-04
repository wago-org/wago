package wasmcall

import (
	"strings"
	"testing"

	"github.com/wago-org/wago"
)

func TestValidateSignatureRejectsValuesWithoutCLITextEncoding(t *testing.T) {
	for _, test := range []struct {
		name    string
		params  []wago.ValType
		results []wago.ValType
		want    string
	}{
		{name: "v128 parameter", params: []wago.ValType{wago.ValV128}, want: "parameter 0 is v128"},
		{name: "reference parameter", params: []wago.ValType{wago.ValFuncRef}, want: "parameter 0 is funcref"},
		{name: "v128 result", results: []wago.ValType{wago.ValV128}, want: "result 0 is v128"},
		{name: "reference result", results: []wago.ValType{wago.ValExternRef}, want: "result 0 is externref"},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateSignature(test.params, test.results)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ValidateSignature error = %v, want %q", err, test.want)
			}
		})
	}
	if err := ValidateSignature(
		[]wago.ValType{wago.ValI32, wago.ValI64},
		[]wago.ValType{wago.ValF32, wago.ValF64},
	); err != nil {
		t.Fatalf("scalar signature rejected: %v", err)
	}
}

func TestParseArgsRejectsUnsupportedParameterBeforeSlotMarshalling(t *testing.T) {
	if _, err := ParseArgs([]string{"0"}, []wago.ValType{wago.ValV128}); err == nil || !strings.Contains(err.Error(), "v128") {
		t.Fatalf("ParseArgs v128 error = %v", err)
	}
}
