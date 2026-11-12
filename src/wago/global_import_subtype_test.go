//go:build (linux && (amd64 || arm64)) || (darwin && arm64)

package wago

import (
	"context"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Each module has a global g and exports read (cast to i31, then i31.get_s)
// and is_null. The allocation in read exercises the shared collector domain.
func globalImportSubtypeModule(heap byte, nullable, imported, mutable bool) []byte {
	typ := []byte{0x64, heap, 0} // (global (ref heap))
	if nullable {
		typ[0] = 0x63
	}
	if mutable {
		typ[2] = 1
	}
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(
		[]byte{0x5f, 0x00}, // (struct)
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
	))}
	if imported {
		entry := append(wasmtest.Name("env"), wasmtest.Name("g")...)
		entry = append(entry, 3)
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(append(entry, typ...))))
	}
	sections = append(sections, wasmtest.Section(3, []byte{2, 1, 1}))
	if !imported {
		init := []byte{0x41, 0x79, 0xfb, 0x1c, 0x0b} // i32.const -7; ref.i31; end
		if heap == 0x71 {                            // none has only a null inhabitant
			init = []byte{0xd0, heap, 0x0b}
		}
		sections = append(sections, wasmtest.Section(6, wasmtest.Vec(append(typ, init...))))
	}
	sections = append(sections,
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("g", 3, 0),
			wasmtest.ExportEntry("read", 0, 0),
			wasmtest.ExportEntry("is_null", 0, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{
				0xfb, 0x00, 0x00, 0x1a, // struct.new 0; drop
				0x23, 0x00, // global.get 0
				0xfb, 0x16, 0x6c, // ref.cast (ref i31)
				0xfb, 0x1d, 0x0b, // i31.get_s; end
			}),
			wasmtest.Code([]byte{0x23, 0, 0xd1, 0x0b}), // global.get 0; ref.is_null; end
		)),
	)
	return wasmtest.Module(sections...)
}

func TestGlobalImportReferenceSubtype(t *testing.T) {
	const i31, eq, anyref, none = 0x6c, 0x6d, 0x6e, 0x71
	cases := []struct {
		name                     string
		from, to                 byte
		fromNullable, toNullable bool
		fromMutable, toMutable   bool
		wantError                bool
	}{
		{"i31", i31, i31, true, true, false, false, false},
		{"i31 to eq", i31, eq, true, true, false, false, false},
		{"i31 to any", i31, anyref, true, true, false, false, false},
		{"non-null i31 to non-null eq", i31, eq, false, false, false, false, false},
		{"non-null i31 to any", i31, anyref, false, true, false, false, false},
		{"null-only to i31", none, i31, true, true, false, false, false},
		{"nullable to non-null", i31, anyref, true, false, false, false, true},
		{"null-only to non-null", none, i31, true, false, false, false, true},
		{"any to i31", anyref, i31, true, true, false, false, true},
		{"mutable i31", i31, i31, true, true, true, true, false},
		{"mutable i31 to any", i31, anyref, true, true, true, true, true},
		{"mutable any to i31", anyref, i31, true, true, true, true, true},
		{"immutable to mutable", i31, anyref, true, true, false, true, true},
		{"mutable to immutable", i31, anyref, true, true, true, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)))
			defer rt.Close()
			providerModule, err := rt.Compile(globalImportSubtypeModule(tc.from, tc.fromNullable, false, tc.fromMutable))
			if err != nil {
				t.Fatal(err)
			}
			defer providerModule.Close()
			provider, err := rt.Instantiate(context.Background(), providerModule)
			if err != nil {
				t.Fatal(err)
			}
			defer provider.Close()
			g, err := provider.ExportedGlobalObject("g")
			if err != nil {
				t.Fatal(err)
			}
			mod, err := rt.Compile(globalImportSubtypeModule(tc.to, tc.toNullable, true, tc.toMutable))
			if err != nil {
				t.Fatal(err)
			}
			defer mod.Close()
			consumer, err := rt.Instantiate(context.Background(), mod, WithImports(NewImports().Global("env", "g", g)))
			if consumer != nil {
				defer consumer.Close()
			}
			if tc.wantError {
				if consumer != nil || err == nil {
					t.Fatalf("Instantiate = %v, %v; want an incompatible-global error", consumer, err)
				}
				assertGlobalSubtypeRead(t, provider, tc.from == none)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if reexport, err := consumer.ExportedGlobalObject("g"); err != nil || reexport != g {
				t.Fatalf("re-export = %p, %v; want original global %p", reexport, err, g)
			}
			assertGlobalSubtypeRead(t, consumer, tc.from == none)
			if err := provider.Close(); err != nil {
				t.Fatal(err)
			}
			assertGlobalSubtypeRead(t, consumer, tc.from == none)
		})
	}
}

func assertGlobalSubtypeRead(t *testing.T, in *Instance, null bool) {
	t.Helper()
	name, want := "read", int32(-7)
	bits := uint64(NewI31Ref(-7).bits)
	if null {
		name, want, bits = "is_null", 1, 0
	}
	got, err := in.Invoke(name)
	if err != nil || len(got) != 1 || AsI32(got[0]) != want {
		t.Fatalf("%s = %v, %v; want %d", name, got, err, want)
	}
	value, err := in.GlobalValue("g")
	if err != nil || value.Bits() != bits {
		t.Fatalf("GlobalValue = %v, %v; want bits %#x", value, err, bits)
	}
}

func TestGlobalImportReferenceSubtypeConcurrentReuse(t *testing.T) {
	rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)))
	t.Cleanup(func() { _ = rt.Close() })
	gcConfig := GCConfig{Profile: GCProfileThroughput, StressNurseryBytes: 64, CollectEveryAlloc: true, ForceMajorEveryMinor: true, VerifyAfterCollect: true, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096}
	providerModule, err := rt.Compile(globalImportSubtypeModule(0x6c, true, false, false))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = providerModule.Close() })
	provider, err := rt.Instantiate(context.Background(), providerModule, WithGC(gcConfig))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	g, err := provider.ExportedGlobalObject("g")
	if err != nil {
		t.Fatal(err)
	}
	mod, err := rt.Compile(globalImportSubtypeModule(0x6e, true, true, false))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mod.Close() })
	imports := NewImports().Global("env", "g", g)
	t.Cleanup(func() {
		assertGlobalSubtypeRead(t, provider, false)
		g.owner.mu.Lock()
		importers := g.owner.importers
		g.owner.mu.Unlock()
		if importers != 0 {
			t.Fatalf("closed consumers retained %d global attachments", importers)
		}
	})
	for worker := 0; worker < 8; worker++ {
		t.Run(fmt.Sprint(worker), func(t *testing.T) {
			t.Parallel()
			for iteration := 0; iteration < 16; iteration++ {
				consumer, err := rt.Instantiate(context.Background(), mod, WithImports(imports), WithGC(gcConfig))
				if err != nil {
					t.Fatal(err)
				}
				assertGlobalSubtypeRead(t, consumer, false)
				if err := consumer.Close(); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
