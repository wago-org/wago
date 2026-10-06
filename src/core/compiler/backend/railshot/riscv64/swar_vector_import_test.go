//go:build linux && !tinygo

package riscv64

import (
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestSWARPublicSelectorBoundsBeforeScanning(t *testing.T) {
	oversized := make([]byte, MaxModuleBytes+1)
	for _, module := range []*wasm.Module{
		nil,
		{Globals: make([]wasm.Global, 257)},
		{Code: []wasm.Func{{BodyBytes: oversized}}},
		{Code: []wasm.Func{{BodyBytes: oversized[:MaxModuleBytes]}}, Data: []wasm.Data{{Init: []byte{1}}}},
		{Data: make([]wasm.Data, 257)},
	} {
		if yes, err := UsesSWAR(module); yes || !errors.Is(err, ErrUnsupported) {
			t.Fatal("unbounded decoded selector", yes, err)
		}
	}
	for _, module := range []*wasm.Module{
		{Globals: []wasm.Global{{Type: wasm.GlobalType{Type: wasm.V128}}}},
		{Code: []wasm.Func{{BodyBytes: []byte{0xfd, 12}}}},
	} {
		if yes, err := UsesSWAR(module); !yes || err != nil {
			t.Fatal("bounded actual SIMD profile", yes, err)
		}
	}
}
