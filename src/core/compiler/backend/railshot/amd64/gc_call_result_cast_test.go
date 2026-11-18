//go:build amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func callResultCastModule(t *testing.T, targetType uint32) *wasm.Module {
	t.Helper()
	b := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x50, 0, 0x5f, 0},
			[]byte{0x50, 1, 0, 0x5f, 0},
			[]byte{0x60, 0, 1, 0x64, 1}, // (func (result (ref 1)))
			wasmtest.FuncType(nil, []wasm.ValType{wasm.AnyRef}),
		)),
		wasmtest.Section(3, wasmtest.Vec([]byte{2}, []byte{3})),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0xfb, 0, 1, 0x0b}),
			wasmtest.Code([]byte{0x10, 0, 0xfb, 22, byte(targetType), 0x0b}),
		)),
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

func fieldResultCastModule(t *testing.T) *wasm.Module {
	t.Helper()
	b := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x50, 0, 0x5f, 0},
			[]byte{0x50, 1, 0, 0x5f, 1, 0x64, 0, 0},
			[]byte{0x60, 1, 0x64, 1, 1, 0x6e},
		)),
		wasmtest.Section(3, wasmtest.Vec([]byte{2})),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0xfb, 2, 1, 0, 0xfb, 22, 0, 0x0b}),
		)),
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

func TestGCCallResultCastElided(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, tc := range []struct {
		name   string
		target uint32
	}{
		{name: "same-type", target: 1},
		{name: "supertype", target: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stats ModuleStats
			cm, err := CompileModuleWith(callResultCastModule(t, tc.target), CompileOptions{
				GCStructHelpers: true,
				Stats:           &stats,
				Optimizations:   map[string]bool{"inline": false},
			})
			if err != nil {
				t.Fatal(err)
			}
			defer cm.CodeImage.Close()
			if got := stats.Funcs[1].Peephole["gc-call-result-cast-elide"]; got != 1 {
				t.Fatalf("proved call-result cast elisions=%d want1", got)
			}
		})
	}
}

func TestGCCallResultCastProofLimits(t *testing.T) {
	m := callResultCastModule(t, 1)
	indexed := func(index uint32, nullable, recursive, exact bool) *wasm.CompType {
		return &wasm.CompType{Results: []wasm.ValType{
			wasm.RefVal(wasm.Ref(nullable, wasm.IndexedHeap(wasm.TypeIdx{Index: index, Rec: recursive}), exact)),
		}}
	}
	tests := []struct {
		name string
		m    *wasm.Module
		ft   *wasm.CompType
		body []byte
		want bool
	}{
		{name: "same-type", m: m, ft: indexed(1, false, false, false), body: []byte{0xfb, 22, 1, 0x01}, want: true},
		{name: "supertype", m: m, ft: indexed(1, false, false, false), body: []byte{0xfb, 22, 0, 0x01}, want: true},
		{name: "exact-source", m: m, ft: indexed(1, false, false, true), body: []byte{0xfb, 22, 1}, want: true},
		{name: "nullable-source", m: m, ft: indexed(1, true, false, false), body: []byte{0xfb, 22, 1}},
		{name: "downcast", m: m, ft: indexed(0, false, false, false), body: []byte{0xfb, 22, 1}},
		{name: "unrelated-target", m: m, ft: indexed(1, false, false, false), body: []byte{0xfb, 22, 2}},
		{name: "exact-target", m: m, ft: indexed(1, false, false, false), body: []byte{0xfb, 22, 0x62, 1}},
		{name: "recursive-source", m: m, ft: indexed(1, false, true, false), body: []byte{0xfb, 22, 1}},
		{name: "abstract-source", m: m, ft: &wasm.CompType{Results: []wasm.ValType{wasm.AnyRef}}, body: []byte{0xfb, 22, 1}},
		{name: "abstract-target", m: m, ft: indexed(1, false, false, false), body: []byte{0xfb, 22, 0x6d}},
		{name: "nullable-cast", m: m, ft: indexed(1, false, false, false), body: []byte{0xfb, 23, 1}},
		{name: "ref-test", m: m, ft: indexed(1, false, false, false), body: []byte{0xfb, 20, 1}},
		{name: "non-reference-result", m: m, ft: &wasm.CompType{Results: []wasm.ValType{wasm.I32}}, body: []byte{0xfb, 22, 1}},
		{name: "multiple-results", m: m, ft: &wasm.CompType{Results: []wasm.ValType{wasm.AnyRef, wasm.I32}}, body: []byte{0xfb, 22, 1}},
		{name: "no-results", m: m, ft: &wasm.CompType{}, body: []byte{0xfb, 22, 1}},
		{name: "nil-signature", m: m, body: []byte{0xfb, 22, 1}},
		{name: "nil-module", ft: indexed(1, false, false, false), body: []byte{0xfb, 22, 1}},
		{name: "other-opcode", m: m, ft: indexed(1, false, false, false), body: []byte{0x01}},
		{name: "truncated-prefix", m: m, ft: indexed(1, false, false, false), body: []byte{0xfb}},
		{name: "truncated-target", m: m, ft: indexed(1, false, false, false), body: []byte{0xfb, 22}},
		{name: "invalid-exact-abstract", m: m, ft: indexed(1, false, false, false), body: []byte{0xfb, 22, 0x62, 0x6d}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := wasm.ReaderFrom(tc.body)
			if got := consumeProvedGCCallResultCast(tc.m, &r, tc.ft); got != tc.want {
				t.Fatalf("proof=%t want%t", got, tc.want)
			}
			wantOffset := 0
			if tc.want {
				wantOffset = 3
			}
			if got := r.Offset(); got != wantOffset {
				t.Fatalf("reader offset=%d want%d", got, wantOffset)
			}
		})
	}
}

func TestGCFieldResultCastElided(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, tc := range []struct {
		name   string
		shared bool
	}{
		{name: "direct", shared: true},
		{name: "helper", shared: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			saved := gcSharedStubsEnabled
			gcSharedStubsEnabled = tc.shared
			defer func() { gcSharedStubsEnabled = saved }()

			var stats ModuleStats
			cm, err := CompileModuleWith(fieldResultCastModule(t), CompileOptions{
				GCStructHelpers: true,
				Stats:           &stats,
			})
			if err != nil {
				t.Fatal(err)
			}
			defer cm.CodeImage.Close()
			if got := stats.Funcs[0].Peephole["gc-field-result-cast-elide"]; got != 1 {
				t.Fatalf("proved field-result cast elisions=%d want1", got)
			}
		})
	}
}
