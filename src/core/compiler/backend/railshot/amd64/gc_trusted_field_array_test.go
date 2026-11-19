//go:build amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func trustedFieldArrayGetModule(t *testing.T, trusted bool) *wasm.Module {
	t.Helper()
	param := []byte{0x64, 1} // (ref 1)
	body := []byte{
		0x20, 0, // local.get 0
		0xfb, 22, 1, // ref.cast (ref 1)
		0xfb, 2, 1, 0, // struct.get 1 0
	}
	if !trusted {
		param = []byte{0x6d}   // eqref
		body = []byte{0x20, 0} // local.get 0
	}
	body = append(body,
		0xfb, 22, 0, // ref.cast (ref 0)
		0x41, 0, // i32.const 0
		0xfb, 11, 0, // array.get 0
		0x0b,
	)
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x4f, 0, 0x5e, 0x7f, 1},    // final (array (mut i32))
			[]byte{0x4f, 0, 0x5f, 1, 0x6d, 1}, // final (struct (field (mut eqref)))
			append(append([]byte{0x60, 1}, param...), 1, 0x7f),
		)),
		wasmtest.Section(3, wasmtest.Vec([]byte{2})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	if err = wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestGCTrustedFieldArrayGet(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, tc := range []struct {
		name          string
		trusted       bool
		optimizations map[string]bool
		want          int
	}{
		{"collector field", true, nil, 1},
		{"facts disabled", true, map[string]bool{"value-facts": false}, 0},
		{"untrusted parameter", false, nil, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := trustedFieldArrayGetModule(t, tc.trusted)
			metadata, err := frontend.BuildGCTypeMetadata(m)
			if err != nil {
				t.Fatal(err)
			}
			var stats ModuleStats
			cm, err := CompileModuleWith(m, CompileOptions{
				GCStructHelpers: true,
				GCArrayHelpers:  true,
				Stats:           &stats,
				Optimizations:   tc.optimizations,
				Codegen: codegen.Options{Module: codegen.ModuleInfo{
					GCTypeDescs:   metadata.Descs,
					GCTypeLayouts: metadata.Layouts,
				}},
			})
			if err != nil {
				t.Fatal(err)
			}
			defer cm.CodeImage.Close()
			if got := stats.Funcs[0].Peephole["gc-trusted-field-array-get"]; got != tc.want {
				t.Fatalf("trusted field-to-array reads=%d want%d", got, tc.want)
			}
		})
	}
}

func trustedStoredCastSharedModule(t *testing.T, trusted bool) *wasm.Module {
	t.Helper()
	param := []byte{0x64, 1} // (ref 1)
	body := []byte{
		0x20, 0, // local.get 0
		0xfb, 22, 1, // ref.cast (ref 1)
		0xfb, 2, 1, 0, // struct.get 1 0
	}
	if !trusted {
		param = []byte{0x6d}   // eqref
		body = []byte{0x20, 0} // local.get 0
	}
	body = append(body,
		0xfb, 22, 0, // ref.cast (ref 0)
		0x0b,
	)
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x4f, 0, 0x5e, 0x7f, 1},    // final (array (mut i32))
			[]byte{0x4f, 0, 0x5f, 1, 0x6d, 1}, // final (struct (field (mut eqref)))
			append(append([]byte{0x60, 1}, param...), 1, 0x64, 0),
		)),
		wasmtest.Section(3, wasmtest.Vec([]byte{2})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	if err = wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestGCTrustedStoredCastShared(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, tc := range []struct {
		name          string
		trusted       bool
		shared        bool
		optimizations map[string]bool
		want          int
	}{
		{"collector field", true, true, nil, 1},
		{"shared stubs disabled", true, false, nil, 0},
		{"facts disabled", true, true, map[string]bool{"value-facts": false}, 0},
		{"untrusted parameter", false, true, nil, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			saved := gcSharedStubsEnabled
			gcSharedStubsEnabled = tc.shared
			defer func() { gcSharedStubsEnabled = saved }()
			m := trustedStoredCastSharedModule(t, tc.trusted)
			metadata, err := frontend.BuildGCTypeMetadata(m)
			if err != nil {
				t.Fatal(err)
			}
			var stats ModuleStats
			cm, err := CompileModuleWith(m, CompileOptions{
				GCStructHelpers: true,
				Stats:           &stats,
				Optimizations:   tc.optimizations,
				Codegen: codegen.Options{Module: codegen.ModuleInfo{
					GCTypeDescs:   metadata.Descs,
					GCTypeLayouts: metadata.Layouts,
				}},
			})
			if err != nil {
				t.Fatal(err)
			}
			defer cm.CodeImage.Close()
			if got := stats.Funcs[0].Peephole["gc-trusted-stored-cast"]; got != tc.want {
				t.Fatalf("trusted stored casts=%d want%d", got, tc.want)
			}
		})
	}
}

func trustedGuardFieldArrayGetModule(t *testing.T) *wasm.Module {
	t.Helper()
	body := []byte{
		0x20, 1, // local.get index
		0x20, 0, // local.get object
		0xfb, 22, 1, // ref.cast (ref 1)
		0xfb, 2, 1, 1, // struct.get 1 1
		0x4f,       // i32.ge_u
		0x04, 0x7f, // if (result i32)
		0x00,    // unreachable
		0x05,    // else
		0x20, 0, // local.get object
		0xfb, 22, 1, // ref.cast (ref 1)
		0xfb, 2, 1, 0, // struct.get 1 0
		0xfb, 22, 0, // ref.cast (ref 0)
		0x20, 1, // local.get index
		0xfb, 11, 0, // array.get 0
		0x0b, // end if
		0x0b, // end function
	}
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x4f, 0, 0x5e, 0x7f, 1},             // final (array (mut i32))
			[]byte{0x4f, 0, 0x5f, 2, 0x6d, 1, 0x7f, 1}, // final (struct (field (mut eqref)) (field (mut i32)))
			[]byte{0x60, 2, 0x64, 1, 0x7f, 1, 0x7f},    // (func (param (ref 1) i32) (result i32))
		)),
		wasmtest.Section(3, wasmtest.Vec([]byte{2})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	if err = wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestGCTrustedTrappingGuardFieldArrayGet(t *testing.T) {
	requireCompilerDiagnostics(t)
	m := trustedGuardFieldArrayGetModule(t)
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	var stats ModuleStats
	cm, err := CompileModuleWith(m, CompileOptions{
		GCStructHelpers: true,
		GCArrayHelpers:  true,
		Stats:           &stats,
		Codegen: codegen.Options{Module: codegen.ModuleInfo{
			GCTypeDescs:   metadata.Descs,
			GCTypeLayouts: metadata.Layouts,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cm.CodeImage.Close()
	if got := stats.Funcs[0].Peephole["gc-trapping-guard-reuse"]; got != 1 {
		t.Fatalf("trapping guard reuse=%d want1", got)
	}
	if got := stats.Funcs[0].Peephole["gc-trusted-field-array-get"]; got != 1 {
		t.Fatalf("trusted guard field-to-array reads=%d want1", got)
	}
}
