//go:build amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestGCGeneralArrayLenEligibility(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, local := range []bool{false, true} {
		code := wasmtest.Code([]byte{0x20, 0, 0xfb, 15, 0x0b})
		if local {
			body := []byte{1, 1, 0x6a, 0x20, 0, 0x21, 1, 0x20, 1, 0xfb, 15, 0x0b}
			code = append(wasmtest.ULEB(uint32(len(body))), body...)
		}
		data := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec([]byte{0x5e, 0x7f, 1}, []byte{0x60, 1, 0x6a, 1, 0x7f})),
			wasmtest.Section(3, wasmtest.Vec([]byte{1})),
			wasmtest.Section(10, wasmtest.Vec(code)),
		)
		m, err := wasm.DecodeModule(data)
		if err != nil {
			t.Fatal(err)
		}
		if err := wasm.ValidateModule(m); err != nil {
			t.Fatal(err)
		}
		var stats ModuleStats
		c, err := CompileModuleWith(m, CompileOptions{GCStructHelpers: true, GCArrayHelpers: true, Stats: &stats})
		if err != nil {
			t.Fatal(err)
		}
		c.CodeImage.Close()
		want := 0
		if gcSharedStubsEnabled {
			want = 1
		}
		if stats.Funcs[0].Peephole["gc-array-len-native"] != want {
			t.Fatalf("local=%v native array.len sites=%d, want %d", local, stats.Funcs[0].Peephole["gc-array-len-native"], want)
		}
	}
}

func TestGCGeneralArrayLenRespectsSharedStubSwitch(t *testing.T) {
	requireCompilerDiagnostics(t)
	saved := gcSharedStubsEnabled
	gcSharedStubsEnabled = false
	defer func() { gcSharedStubsEnabled = saved }()
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 1, 0x6a, 1, 0x7f})),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0xfb, 15, 0x0b}))),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	var stats ModuleStats
	c, err := CompileModuleWith(m, CompileOptions{GCStructHelpers: true, GCArrayHelpers: true, Stats: &stats})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CodeImage.Close()
	if stats.GCSharedStubs != 0 || stats.GCSharedStubCallSites != 0 || stats.Funcs[0].Peephole["gc-array-len-native"] != 0 {
		t.Fatal("disabled shared stubs still emitted an array.len leaf")
	}
}
