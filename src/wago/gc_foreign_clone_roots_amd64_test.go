//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"context"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func foreignCloneBoundaryModule(source bool) []byte {
	structType := []byte{0x5f, 0x02, 0x63, 0x00, 0x01, 0x7f, 0x01}
	if source {
		// Two mutually linked objects, so the second allocation collects while the
		// first clone is still private. Each payload identifies its object.
		body := []byte{0x01, 0x02, 0x63, 0x00,
			0xfb, 0x01, 0x00, 0x21, 0x00, 0xfb, 0x01, 0x00, 0x21, 0x01,
			0x20, 0x00, 0x20, 0x01, 0xfb, 0x05, 0x00, 0x00,
			0x20, 0x01, 0x20, 0x00, 0xfb, 0x05, 0x00, 0x00,
			0x20, 0x00, 0x41, 0x2a, 0xfb, 0x05, 0x00, 0x01,
			0x20, 0x01, 0x41, 0x2b, 0xfb, 0x05, 0x00, 0x01, 0x20, 0x00, 0x0b}
		return wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(structType, []byte{0x60, 0x00, 0x01, 0x63, 0x00})),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("new", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
		)
	}
	imp := append(append(wasmtest.Name("host"), wasmtest.Name("tick")...), 0x00, 0x01)
	body := []byte{0x01, 0x01, 0x63, 0x00,
		0xfb, 0x01, 0x00, 0x21, 0x01,
		0x20, 0x01, 0x41, 0x37, 0xfb, 0x05, 0x00, 0x01,
		0x20, 0x01, 0x24, 0x00,
		0xfb, 0x01, 0x00, 0x21, 0x01,
		0x20, 0x01, 0x20, 0x00, 0xfb, 0x05, 0x00, 0x01,
		0x10, 0x00,
		0x20, 0x01, 0xfb, 0x02, 0x00, 0x01, 0x0b}
	global := []byte{0x63, 0x00, 0x01, 0xd0, 0x00, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, wasmtest.FuncType(nil, nil), wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(6, wasmtest.Vec(global)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func newForeignCloneBoundaryInstance(t testing.TB, module []byte, config GCConfig, imports *Imports) *Instance {
	t.Helper()
	rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)))
	compiled, err := rt.Compile(module)
	if err != nil {
		rt.Close()
		t.Fatal(err)
	}
	options := []InstantiateOption{WithGC(config)}
	if imports != nil {
		options = append(options, WithImports(imports))
	}
	in, err := rt.Instantiate(context.Background(), compiled, options...)
	if err != nil {
		compiled.Close()
		rt.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { in.Close(); compiled.Close(); rt.Close() })
	return in
}

func foreignCloneSourceToken(t testing.TB, source *Instance) GCRef {
	t.Helper()
	values, err := source.InvokeValues(context.Background(), "new")
	if err != nil || len(values) != 1 {
		t.Fatalf("source new = %v, %v", values, err)
	}
	token := values[0].GCRef()
	t.Cleanup(func() { source.ReleaseGCRef(token) })
	return token
}

func checkForeignCloneCycle(t testing.TB, target *Instance, token GCRef) {
	t.Helper()
	target.refStore.mu.Lock()
	entry, ok := target.refStore.gcByToken[token.token]
	target.refStore.mu.Unlock()
	if !ok || entry.owner != target {
		t.Fatal("clone token has the wrong owner")
	}
	root := target.gc.GlobalSlot(entry.slot)
	child, err := target.gc.StructGet(root, 0)
	if err != nil {
		t.Fatal(err)
	}
	back, err := target.gc.StructGet(child.Ref, 0)
	if err != nil || back.Ref != root {
		t.Fatalf("clone cycle = %v, %v", back, err)
	}
	for _, pair := range []struct {
		ref  gc.Ref
		want uint64
	}{{root, 42}, {child.Ref, 43}} {
		got, err := target.gc.StructGet(pair.ref, 1)
		if err != nil || got.Bits != pair.want {
			t.Fatalf("clone payload = %v, %v; want %d", got, err, pair.want)
		}
	}
}

func TestForeignClonePreservesParkedGuestRoots(t *testing.T) {
	profiles := []struct {
		name   string
		config GCConfig
	}{
		{"throughput", GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 128, VerifyAfterCollect: true}},
		{"tiny", GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 1024, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true}},
	}
	for _, profile := range profiles {
		for _, mode := range []string{"success", "rollback", "publication-cleanup"} {
			t.Run(profile.name+"/"+mode, func(t *testing.T) {
				source := newForeignCloneBoundaryInstance(t, foreignCloneBoundaryModule(true), profile.config, nil)
				sourceToken := foreignCloneSourceToken(t, source)
				var target *Instance
				var cloned GCRef
				imports := testImports("host.tick", slotHostFunc(func(_ HostModule, _, _ []uint64) {
					state := target.existingPublicGCState()
					if state == nil || state.hostActivationCount == 0 {
						t.Fatal("test did not park a guest frame")
					}
					if mode == "rollback" {
						objects, root, err := captureForeignGCGraph(source, sourceToken.token, target)
						if err != nil {
							t.Fatal(err)
						}
						objects = append(objects, gcCloneObject{typeID: ^gc.TypeID(0)})
						invocation := target.lockGCInvocation(newInvocationID())
						_, _, err = restoreForeignGCGraph(target, objects, root)
						invocation.unlock()
						if err == nil {
							t.Fatal("invalid trailing object did not trigger rollback")
						}
						if live := target.gc.Stats().LiveObjects; live != 2 {
							t.Fatalf("rollback retained %d objects; want the guest local and global only", live)
						}
					} else {
						var err error
						cloned, err = target.CloneGCRefFrom(source, sourceToken)
						if err != nil {
							t.Fatal(err)
						}
						checkForeignCloneCycle(t, target, cloned)
						if mode == "publication-cleanup" {
							invocation := target.lockGCInvocation(newInvocationID())
							clearForeignCloneRoot(target, true)
							invocation.unlock()
						}
					}
				}))
				target = newForeignCloneBoundaryInstance(t, foreignCloneBoundaryModule(false), profile.config, imports)
				got, err := target.Invoke("run", 303)
				if err != nil || len(got) != 1 || got[0] != 303 {
					t.Fatalf("guest local after clone = %v, %v; want 303", got, err)
				}
				ref := gc.Ref(uint32(readGlobalObject(target.globalCells[0], target.c.Globals[0].Type)))
				global, err := target.gc.StructGet(ref, 1)
				if err != nil || global.Bits != 55 {
					t.Fatalf("guest global after clone = %v, %v", global, err)
				}
				if !cloned.IsNull() {
					checkForeignCloneCycle(t, target, cloned)
					if err := target.ReleaseGCRef(cloned); err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	}
}

func BenchmarkForeignClone(b *testing.B) {
	for _, profile := range []struct {
		name   string
		config GCConfig
	}{{"throughput", GCConfig{}}, {"tiny", GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16}}} {
		b.Run(profile.name, func(b *testing.B) {
			source := newForeignCloneBoundaryInstance(b, foreignCloneBoundaryModule(true), profile.config, nil)
			token := foreignCloneSourceToken(b, source)
			target := newForeignCloneBoundaryInstance(b, foreignCloneBoundaryModule(true), profile.config, nil)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				cloned, err := target.CloneGCRefFrom(source, token)
				if err != nil {
					b.Fatal(err)
				}
				if err := target.ReleaseGCRef(cloned); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
