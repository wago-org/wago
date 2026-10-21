//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"context"
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Joining a collector through an imported global must not make a newly added
// (mut anyref) root a subtype of an existing (mut i32) root. Otherwise a typed
// scalar write can corrupt a reference field and expose other domain objects.
func TestGCIncrementalRuntimeStructIsolation(t *testing.T) {
	for name, cfg := range map[string]GCConfig{
		"default": {},
		"collect": {CollectEveryAlloc: true, StressNurseryBytes: 128, VerifyAfterCollect: true},
		"tiny":    {Profile: GCProfileTiny, TinyHeapBytes: 1 << 20, VerifyAfterCollect: true},
	} {
		t.Run(name, func(t *testing.T) {
			rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)))
			defer rt.Close()
			// An open numeric root forces the ordinary subtype-interval check.
			numeric := []byte{0x50, 0, 0x5f, 1, 0x7f, 1}
			provider, err := rt.Compile(wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil), numeric, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
				wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{2})),
				wasmtest.Section(6, wasmtest.Vec([]byte{0x6e, 1, 0xd0, 0x6e, 0x0b}, []byte{0x6e, 1, 0xd0, 0x6e, 0x0b})),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("anchor", byte(wasm.ExternGlobal), 0), wasmtest.ExportEntry("init", 0, 0), wasmtest.ExportEntry("private-value", 0, 1))),
				wasmtest.Section(10, wasmtest.Vec(
					wasmtest.Code([]byte{0x41, 37, 0xfb, 0, 1, 0x24, 1, 0x41, 11, 0xfb, 0, 1, 0x24, 0, 0x0b}),
					wasmtest.Code([]byte{0x23, 1, 0xfb, 22, 1, 0xfb, 2, 1, 0, 0x0b}))),
			))
			if err != nil {
				t.Fatal(err)
			}
			defer provider.Close()
			first, err := rt.Instantiate(context.Background(), provider, WithGC(cfg))
			if err != nil {
				t.Fatal(err)
			}
			defer first.Close()
			if _, err := first.Invoke("init"); err != nil {
				t.Fatal(err)
			}
			anchor, err := first.ExportedGlobalObject("anchor")
			if err != nil {
				t.Fatal(err)
			}
			consumer, err := rt.Compile(wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(numeric, []byte{0x5f, 1, 0x6e, 1}, wasmtest.FuncType(nil, nil), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil))),
				wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "anchor", wasm.AnyRef, true))),
				wasmtest.Section(3, wasmtest.Vec([]byte{2}, []byte{3}, []byte{4}, []byte{3}, []byte{3}, []byte{4}, []byte{2})),
				wasmtest.Section(6, wasmtest.Vec([]byte{0x6e, 1, 0xd0, 0x6e, 0x0b})),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("init", 0, 0), wasmtest.ExportEntry("unrelated-read", 0, 1), wasmtest.ExportEntry("unrelated-write", 0, 2), wasmtest.ExportEntry("reference-is-null", 0, 3), wasmtest.ExportEntry("anchor-value", 0, 4), wasmtest.ExportEntry("anchor-write", 0, 5), wasmtest.ExportEntry("cast-only", 0, 6))),
				wasmtest.Section(10, wasmtest.Vec(
					wasmtest.Code([]byte{0xd0, 0x6e, 0xfb, 0, 1, 0x24, 1, 0x0b}),
					wasmtest.Code([]byte{0x23, 1, 0xfb, 22, 0, 0xfb, 2, 0, 0, 0x0b}),
					wasmtest.Code([]byte{0x23, 1, 0xfb, 22, 0, 0x20, 0, 0xfb, 5, 0, 0, 0x0b}),
					wasmtest.Code([]byte{0x23, 1, 0xfb, 22, 1, 0xfb, 2, 1, 0, 0xd1, 0x0b}),
					wasmtest.Code([]byte{0x23, 0, 0xfb, 22, 0, 0xfb, 2, 0, 0, 0x0b}),
					wasmtest.Code([]byte{0x23, 0, 0xfb, 22, 0, 0x20, 0, 0xfb, 5, 0, 0, 0x0b}),
					wasmtest.Code([]byte{0x23, 1, 0xfb, 22, 0, 0x1a, 0x0b}))),
			))
			if err != nil {
				t.Fatal(err)
			}
			defer consumer.Close()
			before := first.gc.NativeView().SubtypeIntervalCount
			in, err := rt.Instantiate(context.Background(), consumer, WithGC(cfg), WithImports(testImports("env.anchor", anchor)))
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			numericType, numericOK := in.gcTypeMap.domain(0)
			referenceType, referenceOK := in.gcTypeMap.domain(1)
			t.Logf("type domain: before=%d numeric=%d reference=%d after=%d", before, numericType, referenceType, in.gc.NativeView().SubtypeIntervalCount)
			if in.gc != first.gc || !numericOK || !referenceOK || uint32(numericType) >= before || uint32(referenceType) < before {
				t.Fatal("fixture did not reuse the numeric root and append an unrelated reference root to the imported collector")
			}
			if _, err := in.Invoke("init"); err != nil {
				t.Fatal(err)
			}
			for _, call := range []struct {
				name string
				args []uint64
			}{{"cast-only", nil}, {"unrelated-read", nil}, {"unrelated-write", []uint64{15}}} {
				// 15 is a valid i31 encoding if a broken cast permits the write,
				// so the regression cannot introduce an invalid collector root.
				got, err := in.Invoke(call.name, call.args...)
				var trap *TrapError
				if !errors.As(err, &trap) || trap.Code != TrapCastFailure {
					t.Errorf("%s = %v, %v; want cast failure", call.name, got, err)
				}
			}
			for _, check := range []struct {
				in   *Instance
				name string
				want uint64
			}{{in, "reference-is-null", 1}, {in, "anchor-value", 11}, {first, "private-value", 37}} {
				got, err := check.in.Invoke(check.name)
				if err != nil || len(got) != 1 || got[0] != check.want {
					t.Errorf("%s = %v, %v; want %d", check.name, got, err, check.want)
				}
			}
			if _, err := in.Invoke("anchor-write", 42); err != nil {
				t.Fatal(err)
			}
			if got, err := in.Invoke("anchor-value"); err != nil || len(got) != 1 || got[0] != 42 {
				t.Fatalf("legitimate imported struct write = %v, %v", got, err)
			}
		})
	}
}
