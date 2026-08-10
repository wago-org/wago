//go:build amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func gcLayoutTestModule(groupN, fieldN int) *wasm.Module {
	groups := make([]wasm.RecType, groupN)
	for i := range groups {
		fields := make([]wasm.FieldType, fieldN)
		for j := range fields {
			fields[j] = wasm.NewFieldType(wasm.StorageVal(wasm.I64), wasm.Const)
		}
		groups[i].SubTypes = []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompStruct, Fields: fields}}}
	}
	return &wasm.Module{Types: groups}
}

func gcLayoutHeavyCompileModule(tb testing.TB, sites int) *wasm.Module {
	tb.Helper()
	composite := append([]byte{0x5f}, wasmtest.ULEB(64)...)
	for i := 0; i < 64; i++ {
		composite = append(composite, 0x7e, 0x00) // immutable i64
	}
	body := []byte{0x01, 0x01, 0x63, 0x00}
	for i := 0; i < sites; i++ {
		body = append(body,
			0xfb, 0x01, 0x00, 0x21, 0x00, // struct.new_default type 0; local.set
			0x20, 0x00, 0xfb, 0x02, 0x00, 0x3f, 0x1a, // local.get; struct.get type 0 field 63; drop
		)
	}
	body = append(body, 0x0b)
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(composite, wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		tb.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		tb.Fatal(err)
	}
	return m
}

func TestGCLayoutMetadataMatchesLegacyDerivation(t *testing.T) {
	m := gcLayoutTestModule(8, 12)
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	withMetadata := &fn{m: m, gcTypeLayouts: metadata.Layouts}
	withoutMetadata := &fn{m: m}
	for typeIndex := uint32(0); typeIndex < 8; typeIndex++ {
		for fieldIndex := uint32(0); fieldIndex < 12; fieldIndex++ {
			gotOffset, gotScalar, gotFinal, gotOK := withMetadata.directGCStructLayout(typeIndex, fieldIndex)
			wantOffset, wantScalar, wantFinal, wantOK := withoutMetadata.directGCStructLayout(typeIndex, fieldIndex)
			if gotOffset != wantOffset || gotScalar != wantScalar || gotFinal != wantFinal || gotOK != wantOK {
				t.Fatalf("type %d field %d: metadata=(%d,%+v,%v,%v) legacy=(%d,%+v,%v,%v)", typeIndex, fieldIndex, gotOffset, gotScalar, gotFinal, gotOK, wantOffset, wantScalar, wantFinal, wantOK)
			}
		}
	}

	// The precomputed path must not consult the decoded module after lowering.
	withMetadata.m = nil
	if off, _, final, ok := withMetadata.directGCStructLayout(7, 11); !ok || !final || off != 88 {
		t.Fatalf("metadata-only lookup = (%d,%v,%v), want (88,true,true)", off, final, ok)
	}
	if allocs := testing.AllocsPerRun(100, func() {
		if _, ok := withMetadata.nativeGCStructAllocLayout(7); !ok {
			t.Fatal("native allocation plan unavailable")
		}
	}); allocs != 0 {
		t.Fatalf("precomputed native allocation plan allocs = %v, want 0", allocs)
	}
}

func BenchmarkGCStructFieldLayoutLookup(b *testing.B) {
	m := gcLayoutTestModule(256, 64)
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		b.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		fn   *fn
	}{
		{name: "legacy", fn: &fn{m: m}},
		{name: "precomputed", fn: &fn{m: m, gcTypeLayouts: metadata.Layouts}},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, _, _, ok := tc.fn.directGCStructLayout(255, 63); !ok {
					b.Fatal("layout unavailable")
				}
			}
		})
	}
}

func BenchmarkGCLayoutHeavyCompile(b *testing.B) {
	m := gcLayoutHeavyCompileModule(b, 2000)
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		b.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		layouts []codegen.GCTypeLayout
	}{
		{name: "legacy"},
		{name: "precomputed", layouts: metadata.Layouts},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				cm, err := CompileModuleWith(m, CompileOptions{GCStructHelpers: true, Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeLayouts: tc.layouts}}})
				if err != nil {
					b.Fatal(err)
				}
				b.ReportMetric(float64(len(cm.Code)), "code-bytes")
				benchCompiledSink = cm
			}
		})
	}
}
