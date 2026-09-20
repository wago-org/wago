package ir

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestBuildPreservesDataOperations(t *testing.T) {
	for _, tc := range []struct {
		name string
		body []byte
	}{
		{"memory.init", []byte{0x41, 0, 0x41, 0, 0x41, 1, 0xfc, 8, 0, 0, 0x0b}},
		{"data.drop", []byte{0xfc, 9, 0, 0x0b}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := rawModule(wasm.FuncType{}, tc.body)
			m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
			count := uint32(1)
			m.DataCount = &count
			m.Data = []wasm.Data{{Mode: wasm.DataMode{Kind: wasm.DataPassive}, Init: []byte("x")}}
			if err := wasm.ValidateModule(m); err != nil {
				t.Fatal(err)
			}
			assertBuilds(t, m, tc.name)
		})
	}
}
