package ir

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestBuildUnreachableSelectUnknownType(t *testing.T) {
	for _, op := range []byte{0x9a, 0x8c, 0x79} {
		m := rawModule(wasm.FuncType{}, []byte{0x00, 0x1b, op, 0x1a, 0x0b})
		if err := wasm.ValidateModule(m); err != nil {
			t.Fatal(err)
		}
		assertBuilds(t, m, "trap")
	}
}
