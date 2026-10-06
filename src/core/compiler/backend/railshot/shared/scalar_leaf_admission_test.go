package shared

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestScalarParameterLeafAdmission(t *testing.T) {
	ft := &wasm.CompType{Params: []wasm.ValType{wasm.I32, wasm.I32}, Results: []wasm.ValType{wasm.I32}}
	types := []wasm.ValType{wasm.I32, wasm.I32}
	leaf := []byte{0x20, 0, 0x20, 1, 0x6a, 0x0b}
	for _, body := range [][]byte{leaf, append([]byte{0x01, 0x01, 0x01}, leaf...), {0x20, 0, 0x0b}, {0x41, 7, 0x0b}} {
		if AdmitScalar(body, ft, types).Eligible {
			t.Fatalf("simple parameter leaf admitted: %x", body)
		}
	}
	// Actual local state and additional deferred arithmetic remain in the pilot.
	if !AdmitScalar(leaf, ft, append(types, wasm.I32)).Eligible {
		t.Fatal("declared-local function rejected")
	}
	scaled := []byte{0x20, 0, 0x20, 1, 0x41, 2, 0x74, 0x6a, 0x0b}
	if !AdmitScalar(scaled, ft, types).Eligible {
		t.Fatal("deferred scaled add rejected")
	}
	ft.Params = ft.Params[:1]
	if !AdmitScalar([]byte{0x20, 0, 0x41, 1, 0x6a, 0x0b}, ft, types[:1]).Eligible {
		t.Fatal("single-argument scalar fixture rejected")
	}
}
