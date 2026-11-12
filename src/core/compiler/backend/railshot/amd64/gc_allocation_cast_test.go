//go:build amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func allocationCastModule(t *testing.T, array bool) *wasm.Module {
	t.Helper()
	objectType := []byte{0x5f, 0}
	body := []byte{0xfb, 0, 0}
	if array {
		objectType = []byte{0x5e, 0x7f, 1}
		body = []byte{0x41, 7, 0x41, 1, 0xfb, 6, 0}
	}
	body = append(body, 0xfb, 22, 0, 0x0b)
	b := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			objectType,
			wasmtest.FuncType(nil, []wasm.ValType{wasm.AnyRef}),
		)),
		wasmtest.Section(3, wasmtest.Vec([]byte{1})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	m, err := wasm.DecodeModule(b)
	if err != nil {
		t.Fatal(err)
	}
	if err = wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestGCAllocationSameTypeCastElided(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, array := range []bool{false, true} {
		name := "struct"
		if array {
			name = "array"
		}
		t.Run(name, func(t *testing.T) {
			var stats ModuleStats
			cm, err := CompileModuleWith(allocationCastModule(t, array), CompileOptions{
				GCStructHelpers: true,
				GCArrayHelpers:  true,
				Stats:           &stats,
			})
			if err != nil {
				t.Fatal(err)
			}
			defer cm.CodeImage.Close()
			if got := stats.Funcs[0].Peephole["gc-allocation-cast-elide"]; got != 1 {
				t.Fatalf("same-type allocation cast elisions=%d want1", got)
			}
		})
	}
}

func allocationSupertypeCastModule(t *testing.T) *wasm.Module {
	t.Helper()
	b := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x50, 0, 0x5f, 0},
			[]byte{0x50, 1, 0, 0x5f, 0},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.AnyRef}),
		)),
		wasmtest.Section(3, wasmtest.Vec([]byte{2})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0xfb, 0, 1,
			0xfb, 22, 0,
			0x0b,
		}))),
	)
	m, err := wasm.DecodeModule(b)
	if err != nil {
		t.Fatal(err)
	}
	if err = wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestGCAllocationSupertypeCastElided(t *testing.T) {
	requireCompilerDiagnostics(t)
	var stats ModuleStats
	cm, err := CompileModuleWith(allocationSupertypeCastModule(t), CompileOptions{
		GCStructHelpers: true,
		GCArrayHelpers:  true,
		Stats:           &stats,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cm.CodeImage.Close()
	if got := stats.Funcs[0].Peephole["gc-allocation-cast-elide"]; got != 1 {
		t.Fatalf("proved allocation upcast elisions=%d want1", got)
	}
}

func TestConsumeProvedGCAllocationCastLimits(t *testing.T) {
	m := allocationSupertypeCastModule(t)
	for _, tc := range []struct {
		name       string
		encoded    []byte
		sourceType uint32
		want       bool
	}{
		{name: "same", encoded: []byte{0xfb, 22, 1}, sourceType: 1, want: true},
		{name: "supertype", encoded: []byte{0xfb, 22, 0}, sourceType: 1, want: true},
		{name: "padded-subopcode", encoded: []byte{0xfb, 0x96, 0, 1}, sourceType: 1, want: true},
		{name: "unproved-downcast", encoded: []byte{0xfb, 22, 1}, sourceType: 0},
		{name: "unknown-source", encoded: []byte{0xfb, 22, 0}, sourceType: 99},
		{name: "unknown-target", encoded: []byte{0xfb, 22, 99}, sourceType: 1},
		{name: "nullable", encoded: []byte{0xfb, 23, 1}, sourceType: 1},
		{name: "exact", encoded: []byte{0xfb, 22, 0x62, 1}, sourceType: 1},
		{name: "abstract", encoded: []byte{0xfb, 22, 0x6e}, sourceType: 1},
		{name: "not-cast", encoded: []byte{0xfb, 20, 1}, sourceType: 1},
		{name: "truncated", encoded: []byte{0xfb, 22}, sourceType: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := wasm.ReaderFrom(tc.encoded)
			before := r.Offset()
			if got := consumeProvedGCAllocationCast(m, &r, tc.sourceType); got != tc.want {
				t.Fatalf("proof=%t want%t", got, tc.want)
			}
			if tc.want && r.Offset() != len(tc.encoded) {
				t.Fatalf("accepted reader offset=%d want%d", r.Offset(), len(tc.encoded))
			}
			if !tc.want && r.Offset() != before {
				t.Fatalf("rejected reader advanced from%d to%d", before, r.Offset())
			}
		})
	}
}
