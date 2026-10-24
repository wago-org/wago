//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"context"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// The shared fixture builder supports only one-byte value types. These tests
// also need the explicit nullable reference to their struct at type index zero.
func directReferenceABIFuncType(params, results []wasm.ValType) []byte {
	out := []byte{0x60}
	ref := wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: 0}), false))
	for _, types := range [][]wasm.ValType{params, results} {
		out = append(out, wasmtest.ULEB(uint32(len(types)))...)
		for _, typ := range types {
			if wasm.EqualValType(typ, ref) {
				out = append(out, 0x63, 0)
			} else {
				out = append(out, wasm.MustEncodeValType(typ))
			}
		}
	}
	return out
}

// A private target keeps a reference parameter across an allocation. The
// addressable target calls it through its unchanged wrapper body. Each caller
// keeps another reference below its arguments, then keeps the returned reference
// across a further allocation. The start function calls the same direct path.
func directReferenceABIRootsModule() []byte {
	ref := wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: 0}), false))
	types := [][]byte{
		{0x5f, 1, 0x7f, 1},
		directReferenceABIFuncType([]wasm.ValType{ref}, []wasm.ValType{ref}),
		directReferenceABIFuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		directReferenceABIFuncType(nil, nil),
		directReferenceABIFuncType(nil, []wasm.ValType{wasm.FuncRef}),
	}
	bodies := [][]byte{wasmtest.Code([]byte{0x20, 0, 0x10, 6, 0x0b})}
	for kind := 0; kind < 3; kind++ {
		body := []byte{1, 1, 0x63, 0,
			0x41, 7, 0xfb, 0, 0, // hidden reference
			0x20, 0, 0xfb, 0, 0} // reference argument
		switch kind {
		case 0:
			body = append(body, 0x10, 6)
		case 1:
			body = append(body, 0x41, 0, 0x11, 1, 0)
		case 2:
			body = append(body, 0xd2, 0, 0x14, 1)
		}
		body = append(body, 0x21, 1, 0xfb, 1, 0, 0x1a,
			0xfb, 2, 0, 0, // read hidden reference
			0x20, 1, 0xfb, 2, 0, 0, 0x6a, 0x0b)
		bodies = append(bodies, append(wasmtest.ULEB(uint32(len(body))), body...))
	}
	bodies = append(bodies,
		wasmtest.Code([]byte{0x41, 35, 0x10, 1, 0x41, 42, 0x47, 0x04, 0x40, 0x00, 0x0b, 0x0b}),
		wasmtest.Code([]byte{0xd2, 0, 0x0b}),
		wasmtest.Code([]byte{0xfb, 1, 0, 0x1a, 0x20, 0, 0x0b}),
	)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2}, []byte{2}, []byte{2}, []byte{3}, []byte{4}, []byte{1})),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0, 1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("direct", 0, 1), wasmtest.ExportEntry("indirect", 0, 2), wasmtest.ExportEntry("reference", 0, 3), wasmtest.ExportEntry("identity", 0, 5))),
		wasmtest.Section(8, []byte{4}),
		wasmtest.Section(9, wasmtest.Vec([]byte{0, 0x41, 0, 0x0b, 1, 0})),
		wasmtest.Section(10, wasmtest.Vec(bodies...)),
	)
}

func directReferenceABIGCProfiles() []struct {
	name string
	gc   GCConfig
} {
	return []struct {
		name string
		gc   GCConfig
	}{
		{"default", GCConfig{}},
		{"forced", GCConfig{Profile: GCProfileThroughput, StressNurseryBytes: 64, CollectEveryAlloc: true, ForceMajorEveryMinor: true, VerifyAfterCollect: true, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096}},
		{"tiny", GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 1024, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true}},
	}
}

func TestDirectReferenceABIRootsIdentityAndCodec(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("enabled%t", enabled), func(t *testing.T) {
			cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("inline", false).WithOptimization("dead-gc-new", false).WithOptimization("direct-reference-abi", enabled)
			c, err := Compile(cfg, directReferenceABIRootsModule())
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			loaded := publicArtifactRoundTrip(t, c)
			defer loaded.Close()
			for _, candidate := range []*Compiled{c, loaded} {
				if status := candidate.GCNativeRootAdmission(); !status.Required || !status.Exact {
					t.Fatalf("exact roots lost: %+v", status)
				}
				for _, profile := range directReferenceABIGCProfiles() {
					t.Run(profile.name, func(t *testing.T) {
						in, err := Instantiate(candidate, InstantiateOptions{GC: profile.gc})
						if err != nil {
							t.Fatal(err)
						}
						defer in.Close()
						for _, entry := range []string{"direct", "indirect", "reference"} {
							got, err := in.Invoke(entry, 35)
							if err != nil || len(got) != 1 || got[0] != 42 {
								t.Fatalf("%s=%v,%v, want [42]", entry, got, err)
							}
						}
						identity, err := in.InvokeValues(context.Background(), "identity")
						if err != nil || len(identity) != 1 || identity[0].Type() != ValFuncRef || !in.FuncRefMatchesFunction(identity[0].FuncRef(), 0) {
							t.Fatalf("function identity changed: %v,%v", identity, err)
						}
					})
				}
			}
		})
	}
}

// Public metadata is a view, not authority to manufacture a private function
// handle. An added export must not make the new private adapter Wasm-reachable.
func TestDirectReferenceABIPrivateHostBoundary(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("inline", false), directReferenceABIRootsModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	loaded := publicArtifactRoundTrip(t, c)
	defer loaded.Close()
	for _, candidate := range []*Compiled{c, loaded} {
		candidate.Exports["private"] = 6
		in, err := Instantiate(candidate, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		if ex, err := in.ExportedFunc("private"); err == nil || ex != nil {
			t.Fatalf("public metadata exposed private function: %v, %v", ex, err)
		}
		identity, err := in.InvokeValues(context.Background(), "identity")
		if err != nil || len(identity) != 1 {
			t.Fatalf("identity result: %v, %v", identity, err)
		}
		if !in.FuncRefMatchesFunction(identity[0].FuncRef(), 0) || in.FuncRefMatchesFunction(identity[0].FuncRef(), 6) {
			t.Fatal("addressable identity changed to private target")
		}
	}
}

func TestDirectReferenceABIHostReentry(t *testing.T) {
	ref := wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: 0}), false))
	imp := append(append(wasmtest.Name("env"), wasmtest.Name("reenter")...), 0, 2)
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 1}, directReferenceABIFuncType([]wasm.ValType{ref}, []wasm.ValType{ref}), directReferenceABIFuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2}, []byte{2})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("outer", 0, 2), wasmtest.ExportEntry("inner", 0, 3))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x10, 0, 0x1a, 0x20, 0, 0xfb, 2, 0, 0, 0x41, 35, 0x47, 0x04, 0x40, 0x00, 0x0b, 0x20, 0, 0x0b}),
			wasmtest.Code([]byte{0x41, 7, 0xfb, 0, 0, 0x41, 35, 0xfb, 0, 0, 0x10, 1, 0xfb, 2, 0, 0, 0x1a, 0xfb, 2, 0, 0, 0x41, 35, 0x6a, 0x0b}),
			wasmtest.Code([]byte{0xfb, 1, 0, 0xfb, 2, 0, 0, 0x0b}))),
	)
	for _, enabled := range []bool{false, true} {
		c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("inline", false).WithOptimization("dead-gc-new", false).WithOptimization("direct-reference-abi", enabled), data)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		loaded := publicArtifactRoundTrip(t, c)
		defer loaded.Close()
		for _, candidate := range []*Compiled{c, loaded} {
			for _, profile := range directReferenceABIGCProfiles() {
				var in *Instance
				calls := 0
				in, err = Instantiate(candidate, InstantiateOptions{GC: profile.gc, Imports: testImports("env.reenter", slotHostFunc(func(mod HostModule, _, results []uint64) {
					calls++
					got, err := in.InvokeFromHost(context.Background(), mod, "inner")
					if err != nil || len(got) != 1 || got[0] != 0 {
						panic(fmt.Sprintf("inner=%v,%v, want [0]", got, err))
					}
					results[0] = 0
				}))})
				if err != nil {
					t.Fatal(err)
				}
				got, err := in.Invoke("outer")
				in.Close()
				if err != nil || len(got) != 1 || got[0] != 42 || calls != 1 {
					t.Fatalf("enabled%t %s outer=%v,%v calls%d, want [42] and one callback", enabled, profile.name, got, err, calls)
				}
			}
		}
	}
}
