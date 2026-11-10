//go:build amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestGCArrayCopyNativeSelection(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, tc := range []struct {
		name    string
		count   int32
		dynamic bool
		want    int
	}{
		{name: "zero", want: 1},
		{name: "one", count: 1, want: 1},
		{name: "two", count: 2, want: 1},
		{name: "three", count: 3, want: 1},
		{name: "four", count: 4, want: 1},
		{name: "five", count: 5},
		{name: "dynamic", dynamic: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			count := []byte{0x41, byte(tc.count)}
			if tc.dynamic {
				count = []byte{0x20, 4}
			}
			body := []byte{0x20, 0, 0x20, 1, 0x20, 2, 0x20, 3}
			body = append(body, count...)
			body = append(body, 0xfb, 17, 0, 0, 0x0b)
			data := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(
					[]byte{0x4f, 0, 0x5e, 0x7f, 1},
					[]byte{0x60, 5, 0x63, 0, 0x7f, 0x63, 0, 0x7f, 0x7f, 0},
				)),
				wasmtest.Section(3, wasmtest.Vec([]byte{1})),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("copy", 0, 0))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
			)
			m, err := wasm.DecodeModule(data)
			if err != nil {
				t.Fatal(err)
			}
			if err := wasm.ValidateModule(m); err != nil {
				t.Fatal(err)
			}
			if _, final, ok := directGCArrayLayout(m, 0); !ok || !final {
				t.Fatalf("direct array layout: final=%v ok=%v", final, ok)
			}
			var stats ModuleStats
			cm, err := CompileModuleWith(m, CompileOptions{GCArrayHelpers: true, Stats: &stats})
			if err != nil {
				t.Fatal(err)
			}
			defer cm.CodeImage.Close()
			want := tc.want
			if !gcSharedStubsEnabled {
				want = 0
			}
			if got := stats.Funcs[0].Peephole["gc-array-native-copy"]; got != want {
				t.Fatalf("native copies=%d, want %d", got, want)
			}
			dynamic := 0
			if gcSharedStubsEnabled && (tc.dynamic || tc.count > 4) {
				dynamic = 1
			}
			if got := stats.Funcs[0].Peephole["gc-array-native-copy-dynamic"]; got != dynamic {
				t.Fatalf("dynamic copies=%d, want%d", got, dynamic)
			}
			if got, wantResolutions := stats.Funcs[0].GCHandleResolutions, 2*(want+dynamic); got != wantResolutions {
				t.Fatalf("handle resolutions=%d, want %d", got, wantResolutions)
			}
			if got := stats.Funcs[0].GCHandleResolutionReuse; got != 0 {
				t.Fatalf("handle resolution reuse=%d, want 0", got)
			}
		})
	}
}

func TestGCArrayCopyNativeSelectionLayouts(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, tc := range []struct {
		name       string
		arrayTypes [][]byte
		dstType    byte
		srcType    byte
		want       int
	}{
		{name: "distinct-final-i32", arrayTypes: [][]byte{{0x4f, 0, 0x5e, 0x7f, 1}, {0x4f, 0, 0x5e, 0x7f, 1}}, srcType: 1, want: 1},
		{name: "open-i32", arrayTypes: [][]byte{{0x50, 0, 0x5e, 0x7f, 1}}},
		{name: "anyref", arrayTypes: [][]byte{{0x5e, 0x6e, 1}}},
		{name: "v128", arrayTypes: [][]byte{{0x5e, 0x7b, 1}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			types := append([][]byte(nil), tc.arrayTypes...)
			types = append(types, []byte{0x60, 4, 0x63, tc.dstType, 0x7f, 0x63, tc.srcType, 0x7f, 0})
			body := []byte{0x20, 0, 0x20, 1, 0x20, 2, 0x20, 3, 0x41, 1, 0xfb, 17, tc.dstType, tc.srcType, 0x0b}
			data := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(types...)),
				wasmtest.Section(3, wasmtest.Vec([]byte{byte(len(tc.arrayTypes))})),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("copy", 0, 0))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
			)
			m, err := wasm.DecodeModule(data)
			if err != nil {
				t.Fatal(err)
			}
			if err := wasm.ValidateModule(m); err != nil {
				t.Fatal(err)
			}
			var stats ModuleStats
			cm, err := CompileModuleWith(m, CompileOptions{GCArrayHelpers: true, Stats: &stats})
			if err != nil {
				t.Fatal(err)
			}
			defer cm.CodeImage.Close()
			want := tc.want
			if !gcSharedStubsEnabled {
				want = 0
			}
			if got := stats.Funcs[0].Peephole["gc-array-native-copy"]; got != want {
				t.Fatalf("native copies=%d, want %d", got, want)
			}
			if got, wantResolutions := stats.Funcs[0].GCHandleResolutions, 2*want; got != wantResolutions {
				t.Fatalf("handle resolutions=%d, want %d", got, wantResolutions)
			}
		})
	}
}

func TestGCArrayCopyNativeSelectionGlobals(t *testing.T) {
	requireCompilerDiagnostics(t)
	body := []byte{0x23, 0, 0x41, 0, 0x23, 1, 0x41, 0, 0x41, 1, 0xfb, 17, 0, 0, 0x0b}
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x4f, 0, 0x5e, 0x7f, 1},
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec([]byte{1})),
		wasmtest.Section(6, wasmtest.Vec(
			[]byte{0x63, 0, 1, 0xd0, 0, 0x0b},
			[]byte{0x63, 0, 1, 0xd0, 0, 0x0b},
		)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("copy", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	var stats ModuleStats
	cm, err := CompileModuleWith(m, CompileOptions{GCArrayHelpers: true, Stats: &stats})
	if err != nil {
		t.Fatal(err)
	}
	defer cm.CodeImage.Close()
	want := 1
	if !gcSharedStubsEnabled {
		want = 0
	}
	if got := stats.Funcs[0].Peephole["gc-array-native-copy"]; got != want {
		t.Fatalf("native copies=%d, want %d", got, want)
	}
	if got, wantResolutions := stats.Funcs[0].GCHandleResolutions, 2*want; got != wantResolutions {
		t.Fatalf("handle resolutions=%d, want %d", got, wantResolutions)
	}
}
