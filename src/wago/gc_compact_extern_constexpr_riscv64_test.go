//go:build linux && riscv64 && !tinygo && !wago_target_tinygo && !wago_precompiled

package wago

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"

	"testing"
)

func rv64CompactExternConstantModule(vector bool) []byte {
	// The first converted object must survive both later globals and passive
	// entries. The first passive entry must survive the second allocation too.
	global := func(n byte) []byte { return []byte{0x6f, 0, 0x41, n, 0xfb, 0, 0, 0xfb, 27, 0x0b} }
	read := func(index byte) []byte {
		body := []byte{0x23, index, 0xfb, 26, 0xfb, 22, 0, 0xfb, 2, 0, 0, 0x0b}
		if vector {
			prefix := append([]byte{0xfd, 12}, make([]byte, 16)...)
			body = append(append(prefix, 0x1a), body...)
		}
		return wasmtest.Code(body)
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 0}, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{1})),
		wasmtest.Section(6, wasmtest.Vec(global(42), global(7))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("first", 0, 0), wasmtest.ExportEntry("second", 0, 1))),
		wasmtest.Section(9, wasmtest.Vec([]byte{5, 0x6f, 2, 0x41, 33, 0xfb, 0, 0, 0xfb, 27, 0x0b, 0x41, 44, 0xfb, 0, 0, 0xfb, 27, 0x0b})),
		wasmtest.Section(10, wasmtest.Vec(read(0), read(1))),
	)
}
func TestRV64CompactExternConstantPrivate(t *testing.T) {
	for _, vector := range []bool{false, true} {
		c, err := NewRuntimeConfig().Compile(rv64CompactExternConstantModule(vector))
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		loaded := publicArtifactRoundTrip(t, c)
		defer loaded.Close()
		for artifact, candidate := range []*Compiled{c, loaded} {
			for _, profile := range []gc.Profile{gc.ProfileThroughput, gc.ProfileTiny} {
				t.Run(fmt.Sprintf("vector%t/artifact%t/profile%d", vector, artifact != 0, profile), func(t *testing.T) {
					in, err := Instantiate(candidate, InstantiateOptions{GC: GCConfig{Profile: profile, CollectEveryAlloc: true, StressNurseryBytes: 128, VerifyAfterCollect: true}})
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					if err := in.CollectGC(); err != nil {
						t.Fatal(err)
					}
					for _, entry := range []struct {
						name string
						want uint64
					}{{"first", 42}, {"second", 7}} {
						got, err := in.Invoke(entry.name)
						if err != nil || len(got) != 1 || got[0] != entry.want {
							t.Fatalf("%s=%v,%v", entry.name, got, err)
						}
					}
					domain := in.lockGCCollector()
					defer unlockGCCollector(domain)
					state, err := in.compactExternCollector(domain)
					if err != nil {
						t.Fatal(err)
					}
					entries, length := in.gcRootedTableElementSegment(0, ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Nullable: true, Heap: HeapTypeDescriptor{Abstract: AbstractHeapExtern}}})
					if length != 2 {
						t.Fatal("passive length", length)
					}
					for i, want := range []uint64{33, 44} {
						ref, err := state.anyFromExtern(in.gcRootedTableElementBits(entries, uint64(i)), gc.EmptyRoots{})
						if err != nil {
							t.Fatal(err)
						}
						got, err := in.gc.StructGet(ref, 0)
						if err != nil || got.Bits != want {
							t.Fatalf("passive%d=%+v,%v", i, got, err)
						}
					}
				})
			}
		}
	}
}

func TestRV64CompactExternUnusedNullHasNoSidecar(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 0}, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, []byte{1, 1}),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x6f, 1, 0xd0, 0x6f, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xfb, 1, 0, 0x1a, 0x41, 42, 0x0b}))),
	)
	c, err := NewRuntimeConfig().Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	loaded := publicArtifactRoundTrip(t, c)
	defer loaded.Close()
	for _, candidate := range []*Compiled{c, loaded} {
		if compactExternConstructionNeeded(candidate) {
			t.Fatal("unused nullable declaration requested construction journal")
		}
		in, err := Instantiate(candidate, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, VerifyAfterCollect: true}})
		if err != nil {
			t.Fatal(err)
		}
		got, err := in.Invoke("run")
		if err != nil || len(got) != 1 || got[0] != 42 {
			t.Fatal(got, err)
		}
		if _, ok := compactExternCollectors.Load(in.gc); ok {
			t.Fatal("unused null declaration created compact extern sidecar")
		}
		if err := in.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRV64CompactExternStandalonePrivate(t *testing.T) {
	c, err := NewRuntimeConfig().Compile(rv64CompactExternSourceModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	loaded := publicArtifactRoundTrip(t, c)
	defer loaded.Close()
	for _, candidate := range []*Compiled{c, loaded} {
		in, err := Instantiate(candidate, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 128, VerifyAfterCollect: true}})
		if err != nil {
			t.Fatal(err)
		}
		got, err := in.Invoke("local")
		if err != nil || len(got) != 1 || got[0] != 42 {
			t.Fatal(got, err)
		}
		if in.refStore == nil {
			t.Fatal("standalone conversion did not establish its private store")
		}
		if err := in.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
