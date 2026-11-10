//go:build amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestBulkCopyAppendAdmission(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, tc := range []struct {
		name                                   string
		n, dst, src, address, offset           uint32
		between, expression                    []byte
		shared, sse2, noMin, disabled, profile bool
		want                                   int
	}{
		{name: "one-vector", n: 15, src: 1, address: 15, want: 1},
		{name: "two-vectors", n: 31, src: 1, address: 31, want: 1},
		{name: "three-vectors", n: 47, dst: 17, src: 18, address: 64, want: 1},
		{name: "four-vectors", n: 63, dst: 33, src: 34, address: 96, want: 1},
		{name: "memarg-offset", n: 31, dst: 7, src: 8, address: 0, offset: 38, want: 1},
		{name: "last-byte", n: 31, dst: 65504, src: 65505, address: 65535, want: 1},
		{name: "length", n: 32, src: 1, address: 32},
		{name: "source", n: 31, src: 2, address: 31},
		{name: "append-address", n: 31, src: 1, address: 32},
		{name: "append-overflow", n: 31, src: 1, address: 0xffffffff, offset: 32},
		{name: "out-of-bounds", n: 31, dst: 65505, src: 65506, address: 65536},
		{name: "intervening-nop", n: 31, src: 1, address: 31, between: []byte{1}},
		{name: "local-write", n: 31, src: 1, address: 31, expression: []byte{0x20, 0, 0x22, 0}},
		{name: "trapping-value", n: 31, src: 1, address: 31, expression: []byte{0x20, 0, 0x20, 1, 0x6d}},
		{name: "memory-read-value", n: 31, src: 1, address: 31, expression: []byte{0x41, 0, 0x2d, 0, 0}},
		{name: "shared", n: 31, src: 1, address: 31, shared: true},
		{name: "sse2", n: 31, src: 1, address: 31, sse2: true},
		{name: "minimum-proof-disabled", n: 31, src: 1, address: 31, noMin: true},
		{name: "disabled", n: 31, src: 1, address: 31, disabled: true},
		{name: "source-profile", n: 31, src: 1, address: 31, profile: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.profile && !profileEnabled {
				t.Skip("source maps require wago_profile")
			}
			body := []byte{0}
			for _, x := range []uint32{tc.dst, tc.src, tc.n} {
				body = append(body, 0x41)
				body = append(body, wasmtest.SLEB32(int32(x))...)
			}
			body = append(body, 0xfc, 10, 0, 0)
			body = append(body, tc.between...)
			body = append(body, 0x41)
			body = append(body, wasmtest.SLEB32(int32(tc.address))...)
			expr := tc.expression
			if expr == nil {
				expr = []byte{0x20, 0, 0x41, 11, 0x6c, 0x20, 1, 0x6a}
			}
			body = append(body, expr...)
			body = append(body, 0x3a, 0)
			body = append(body, wasmtest.ULEB(tc.offset)...)
			body = append(body, 0x0b)
			m := byteReduceTestModule(t, 1, []wasm.ValType{wasm.I32, wasm.I32}, nil, body)
			m.Memories[0].Shared = tc.shared
			var stats ModuleStats
			cm, err := CompileModuleWith(m, CompileOptions{Stats: &stats, Profile: tc.profile, SourceMaps: tc.profile, AMD64FeaturesSet: tc.sse2, Optimizations: map[string]bool{"bulk-copy-vector": !tc.disabled, "memory-min-bounds": !tc.noMin}})
			if err != nil {
				t.Fatal(err)
			}
			cm.CodeImage.Close()
			if got := stats.Funcs[0].Peephole["bulk-copy-append"]; got != tc.want {
				t.Fatalf("selected=%d want=%d", got, tc.want)
			}
		})
	}
}

func TestBulkCopyAppendInlineSelection(t *testing.T) {
	requireCompilerDiagnostics(t)
	helper := []byte{0x41, 0, 0x41, 1, 0x41, 31, 0xfc, 10, 0, 0, 0x41, 31, 0x20, 0, 0x41, 11, 0x6c, 0x20, 1, 0x6a, 0x3a, 0, 0, 0x0b}
	caller := []byte{0x20, 1, 0x20, 0, 0x10, 0, 0x0b}
	m, err := wasm.DecodeModule(wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, nil))), wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))), wasmtest.Section(5, wasmtest.Vec([]byte{1, 1, 1})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("shift", 0, 1))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(helper), wasmtest.Code(caller)))))
	if err != nil {
		t.Fatal(err)
	}
	var stats ModuleStats
	cm, err := CompileModuleWith(m, CompileOptions{Stats: &stats, Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer cm.CodeImage.Close()
	if got := stats.Funcs[1].Peephole["bulk-copy-append"]; got != 1 {
		t.Fatalf("inlined caller selected=%d want=1", got)
	}
}
