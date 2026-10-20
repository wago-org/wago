//go:build amd64

package amd64

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestModuleSharedNullI31Classifier(t *testing.T) {
	requireCompilerDiagnostics(t)
	const functions, sites = 3, 8
	body := []byte{}
	for i := 0; i < sites; i++ {
		body = append(body, 0x20, 0, 0xfb, 20, 0x6c)
		if i != sites-1 {
			body = append(body, 0x1a)
		}
	}
	body = append(body, 0x0b)
	funcs, exports, bodies := [][]byte{}, [][]byte{}, [][]byte{}
	for i := 0; i < functions; i++ {
		funcs = append(funcs, []byte{0})
		exports = append(exports, wasmtest.ExportEntry(fmt.Sprint(i), 0, uint32(i)))
		bodies = append(bodies, wasmtest.Code(body))
	}
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.AnyRef}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(funcs...)),
		wasmtest.Section(7, wasmtest.Vec(exports...)),
		wasmtest.Section(10, wasmtest.Vec(bodies...)),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	if err = wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	for _, compact := range []bool{false, true} {
		var serial []byte
		for _, workers := range []int{1, 2} {
			t.Run(fmt.Sprintf("compact%t/workers%d", compact, workers), func(t *testing.T) {
				var stats ModuleStats
				cm, err := CompileModuleWith(m, CompileOptions{Workers: workers, CompactNative: compact, GCStructHelpers: true, Stats: &stats})
				if err != nil {
					t.Fatal(err)
				}
				if cm.CodeImage != nil {
					defer cm.CodeImage.Close()
				}
				t.Logf("code=%d stub-bodies=%d stub-sites=%d stub-bytes=%d", len(cm.Code), stats.GCSharedStubs, stats.GCSharedStubCallSites, stats.GCSharedStubBytes)
				if workers == 1 {
					serial = append([]byte(nil), cm.Code...)
				} else if !bytes.Equal(serial, cm.Code) {
					t.Error("serial and parallel code differ")
				}
				if stats.GCSharedStubs != 1 || stats.GCSharedStubCallSites != functions*sites || stats.GCSharedStubBytes == 0 {
					t.Errorf("want one shared classifier for %d sites", functions*sites)
				}
			})
		}
	}
}
