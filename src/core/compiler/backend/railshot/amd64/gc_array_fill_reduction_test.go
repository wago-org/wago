//go:build amd64

package amd64

import (
	"bytes"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func gcArrayFillReductionModuleBytes(ordinary bool) []byte {
	body := []byte{2, 2, 0x7f, 1, 0x63, 0}
	if ordinary {
		body = append(body, 1)
	}
	body = append(body, 0x03, 0x40, 0x20, 2, 0x02, 0x7f,
		0x41, 0, 0x41, 16, 0xfb, 6, 0, 0x21, 3,
		0x20, 3, 0x41, 0, 0x20, 1, 0x41, 16, 0xfb, 16, 0,
		0x20, 3, 0x20, 1, 0x41, 15, 0x71, 0xfb, 11, 0, 0x0b, 0x6a, 0x21, 2,
		0x20, 1, 0x41, 1, 0x6a, 0x21, 1, 0x20, 1, 0x20, 0, 0x49, 0x0d, 0, 0x0b, 0x20, 2, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5e, 0x7f, 1}, wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))))
}

func TestGCArrayFillReductionSelection(t *testing.T) {
	for _, tc := range []struct {
		name                            string
		ordinary, interruptible, source bool
		from, to                        []byte
		want                            bool
	}{
		{name: "accepted", want: true},
		{name: "partial-fill", from: []byte{0x20, 1, 0x41, 16, 0xfb, 16}, to: []byte{0x20, 1, 0x41, 15, 0xfb, 16}},
		{name: "nonzero-start", from: []byte{0x20, 3, 0x41, 0}, to: []byte{0x20, 3, 0x41, 1}},
		{name: "prelude", ordinary: true}, {name: "interruptible", interruptible: true}, {name: "source-profile", source: true},
		{name: "unsafe-index", from: []byte{0x41, 15, 0x71}, to: []byte{0x41, 16, 0x71}},
		{name: "different-step", from: []byte{0x41, 1, 0x6a}, to: []byte{0x41, 2, 0x6a}},
		{name: "signed-bound", from: []byte{0x49, 0x0d}, to: []byte{0x48, 0x0d}},
		{name: "non-power-length", from: []byte{0x41, 16, 0xfb}, to: []byte{0x41, 15, 0xfb}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.source && !profileEnabled {
				t.Skip("source profiles disabled")
			}
			data := gcArrayFillReductionModuleBytes(tc.ordinary)
			if tc.from != nil {
				if !bytes.Contains(data, tc.from) {
					t.Fatal("missing mutation")
				}
				data = bytes.Replace(data, tc.from, tc.to, 1)
			}
			m, err := wasm.DecodeModule(data)
			if err != nil {
				t.Fatal(err)
			}
			if err = wasm.ValidateModule(m); err != nil {
				t.Fatal(err)
			}
			var stats *ModuleStats
			if diagnosticsEnabled {
				stats = &ModuleStats{}
			}
			cm, err := CompileModuleWith(m, CompileOptions{GCStructHelpers: true, GCArrayHelpers: true, Interruptible: tc.interruptible, Profile: tc.source, SourceMaps: tc.source, Stats: stats})
			if err != nil {
				t.Fatal(err)
			}
			defer cm.CodeImage.Close()
			if got := directPreparedMarked(cm.DirectPreparedBounded, 0); got != tc.want {
				t.Fatalf("bounded=%v want%v", got, tc.want)
			}
			if tc.want && directPreparedMarked(cm.DirectPrepared, 0) {
				t.Fatal("helper entry exposed direct register admission")
			}
		})
	}
}
