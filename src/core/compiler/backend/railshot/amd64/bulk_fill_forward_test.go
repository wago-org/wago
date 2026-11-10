//go:build amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestBulkFillCopyForwardSelection(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, tc := range []struct {
		name            string
		between         []byte
		source, count   byte
		enabled, shared bool
		want            int
	}{
		{name: "matched", source: 1, count: 2, enabled: true, want: 1},
		{name: "disabled", source: 1, count: 2},
		{name: "changed-source", source: 3, count: 2, enabled: true},
		{name: "changed-count", source: 1, count: 3, enabled: true},
		{name: "intervening-nop", between: []byte{1}, source: 1, count: 2, enabled: true},
		{name: "intervening-store", between: []byte{0x20, 1, 0x41, 42, 0x3a, 0, 0}, source: 1, count: 2, enabled: true},
		{name: "shared", source: 1, count: 2, enabled: true, shared: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte{0, 0x20, 1, 0x41, 7, 0x20, 2, 0xfc, 11, 0}
			body = append(body, tc.between...)
			body = append(body, 0x20, 0, 0x20, tc.source, 0x20, tc.count, 0xfc, 10, 0, 0, 0x0b)
			m, err := wasm.DecodeModule(wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}, nil))),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
				wasmtest.Section(5, wasmtest.Vec([]byte{1, 1, 1})),
				wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
			))
			if err != nil {
				t.Fatal(err)
			}
			if tc.shared {
				m.Memories[0].Shared = true
			}
			var stats ModuleStats
			cm, err := CompileModuleWith(m, CompileOptions{Stats: &stats, Optimizations: map[string]bool{"bulk-fill-copy-forward": tc.enabled}})
			if err != nil {
				t.Fatal(err)
			}
			cm.CodeImage.Close()
			if got := stats.Funcs[0].Peephole["bulk-fill-copy-forward"]; got != tc.want {
				t.Fatalf("selected=%d want=%d", got, tc.want)
			}
		})
	}
}

func TestBulkFillCopyForwardInlineSelection(t *testing.T) {
	requireCompilerDiagnostics(t)
	helper := []byte{0x20, 1, 0x41, 7, 0x20, 2, 0xfc, 11, 0, 0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 10, 0, 0, 0x0b}
	caller := []byte{0x20, 0, 0x20, 1, 0x20, 2, 0x10, 0, 0x0b}
	m, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{1, 1, 1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("copy", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(helper), wasmtest.Code(caller))),
	))
	if err != nil {
		t.Fatal(err)
	}
	var stats ModuleStats
	cm, err := CompileModuleWith(m, CompileOptions{Stats: &stats, Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer cm.CodeImage.Close()
	if got := stats.Funcs[1].Peephole["bulk-fill-copy-forward"]; got != 1 {
		t.Fatalf("inlined caller selected=%d want=1", got)
	}
}
