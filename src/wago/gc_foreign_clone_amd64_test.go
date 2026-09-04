//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/wasmtest"
)

func gcCloneCycleModule() []byte {
	// (struct (field (mut (ref null 0))) (field (mut i32)))
	structType := []byte{0x5f, 0x02, 0x63, 0x00, 0x01, 0x7f, 0x01}
	warmType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil)
	getType := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})
	global := []byte{0x63, 0x00, 0x01, 0xd0, 0x00, 0x0b}
	warm := []byte{0x01, 0x01, 0x63, 0x00,
		0xfb, 0x01, 0x00, 0x21, 0x01,
		0x20, 0x01, 0x20, 0x01, 0xfb, 0x05, 0x00, 0x00,
		0x20, 0x01, 0x20, 0x00, 0xfb, 0x05, 0x00, 0x01,
		0x20, 0x01, 0x24, 0x00,
		0x20, 0x01, 0x24, 0x01,
		0x0b}
	get := []byte{0x00, 0x23, 0x00, 0xfb, 0x02, 0x00, 0x00, 0xfb, 0x02, 0x00, 0x01, 0x0b}
	same := []byte{0x00, 0x23, 0x00, 0x23, 0x01, 0xd3, 0x0b}

	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, warmType, getType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(2))),
		wasmtest.Section(6, wasmtest.Vec(global, global)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("warm", 0, 0),
			wasmtest.ExportEntry("get", 0, 1),
			wasmtest.ExportEntry("same", 0, 2),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(warm))), warm...),
			append(wasmtest.ULEB(uint32(len(get))), get...),
			append(wasmtest.ULEB(uint32(len(same))), same...),
		)),
	)
}

func instantiateForeignCloneFixture(t *testing.T, cfg *RuntimeConfig, gcCfg GCConfig) (*Runtime, *HostFuncRef, *Module, *Instance) {
	t.Helper()
	rt := NewRuntime(WithRuntimeConfig(cfg))
	owner, err := rt.NewGCHostFuncRef(HostFunc(func(_ HostModule, args, results []uint64) {
		results[0] = args[0]
	}), FuncSig{Params: []ValType{ValAnyRef}, Results: []ValType{ValAnyRef}})
	if err != nil {
		rt.Close()
		t.Fatal(err)
	}
	module, err := rt.Compile(gcHostBridgeModule(t))
	if err != nil {
		owner.Close()
		rt.Close()
		t.Fatal(err)
	}
	in, err := rt.Instantiate(context.Background(), module, WithImports(Imports{"host.echo": owner}), WithGC(gcCfg))
	if err != nil {
		module.Close()
		owner.Close()
		rt.Close()
		t.Fatal(err)
	}
	return rt, owner, module, in
}

func TestForeignRuntimeGCGraphClone(t *testing.T) {
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	profiles := []struct {
		name string
		gc   GCConfig
	}{
		{name: "throughput", gc: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true, StressBarriers: true}},
		{name: "tiny", gc: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 256, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true, StressBarriers: true}},
	}
	for _, tc := range profiles {
		t.Run(tc.name, func(t *testing.T) {
			sourceRT, sourceHost, sourceModule, source := instantiateForeignCloneFixture(t, cfg, tc.gc)
			defer sourceRT.Close()
			defer sourceHost.Close()
			defer sourceModule.Close()
			defer source.Close()
			targetRT, targetHost, targetModule, target := instantiateForeignCloneFixture(t, cfg, tc.gc)
			defer targetRT.Close()
			defer targetHost.Close()
			defer targetModule.Close()
			defer target.Close()

			created, err := source.Call(context.Background(), "new")
			if err != nil || len(created) != 1 || created[0].GCRef().IsNull() {
				t.Fatalf("source new = %v, %v", created, err)
			}
			sourceRef := created[0].GCRef()
			if _, err := target.Call(context.Background(), "read", ValueGCRef(sourceRef)); err == nil || (!strings.Contains(err.Error(), "different collector domain") && !strings.Contains(err.Error(), "invalid or stale")) {
				t.Fatalf("uncloned foreign ingress = %v", err)
			}
			cloned, err := target.CloneGCRefFrom(source, sourceRef)
			if err != nil || cloned.IsNull() {
				t.Fatalf("clone = %#v, %v", cloned, err)
			}
			if got, err := target.Call(context.Background(), "read", ValueGCRef(cloned)); err != nil || !reflect.DeepEqual(got, []Value{ValueI32(42)}) {
				t.Fatalf("target read clone = %v, %v", got, err)
			}
			if err := source.ReleaseGCRef(sourceRef); err != nil {
				t.Fatal(err)
			}
			if err := source.Close(); err != nil {
				t.Fatal(err)
			}
			if got, err := target.Call(context.Background(), "read", ValueGCRef(cloned)); err != nil || !reflect.DeepEqual(got, []Value{ValueI32(42)}) {
				t.Fatalf("target clone after source close = %v, %v", got, err)
			}
			if err := target.ReleaseGCRef(cloned); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestForeignRuntimeGCGraphClonePreservesCycle(t *testing.T) {
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	newInstance := func(t *testing.T) (*Runtime, *Module, *Instance) {
		t.Helper()
		rt := NewRuntime(WithRuntimeConfig(cfg))
		module, err := rt.Compile(gcCloneCycleModule())
		if err != nil {
			rt.Close()
			t.Fatal(err)
		}
		in, err := rt.Instantiate(context.Background(), module, WithGC(GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 512, TinyBlockBytes: 64, TinyCollectEveryAlloc: true, VerifyAfterCollect: true}))
		if err != nil {
			module.Close()
			rt.Close()
			t.Fatal(err)
		}
		return rt, module, in
	}
	sourceRT, sourceModule, source := newInstance(t)
	defer sourceRT.Close()
	defer sourceModule.Close()
	defer source.Close()
	targetRT, targetModule, target := newInstance(t)
	defer targetRT.Close()
	defer targetModule.Close()
	defer target.Close()
	if _, err := source.Invoke("warm", 0x12345678); err != nil {
		t.Fatal(err)
	}
	bits := readGlobalObject(source.globalCells[0], source.c.Globals[0].Type)
	ref := gc.Ref(uint32(bits))
	required := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Heap: HeapTypeDescriptor{Defined: true, TypeIndex: 0}}}
	token, err := source.refStore.issueGCRef(source, ref, required)
	if err != nil {
		t.Fatal(err)
	}
	sourceToken := GCRef{token: token}
	defer source.ReleaseGCRef(sourceToken)
	cloned, err := target.CloneGCRefFrom(source, sourceToken)
	if err != nil {
		t.Fatal(err)
	}
	defer target.ReleaseGCRef(cloned)
	target.refStore.mu.Lock()
	entry := target.refStore.gcByToken[cloned.token]
	target.refStore.mu.Unlock()
	root := target.gc.GlobalSlot(entry.slot)
	self, err := target.gc.StructGet(root, 0)
	if err != nil || self.Ref != root {
		t.Fatalf("cloned cycle = %#x, %v; root %#x", uint32(self.Ref), err, uint32(root))
	}
	value, err := target.gc.StructGet(root, 1)
	if err != nil || uint32(value.Bits) != 0x12345678 {
		t.Fatalf("cloned payload = %#x, %v", value.Bits, err)
	}
}

func TestForeignRuntimeGCGraphCloneRejectsWrongOwnerAndSameStore(t *testing.T) {
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	rt, host, module, first := instantiateForeignCloneFixture(t, cfg, GCConfig{})
	defer rt.Close()
	defer host.Close()
	defer module.Close()
	defer first.Close()
	second, err := rt.Instantiate(context.Background(), module, WithImports(Imports{"host.echo": host}))
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	created, err := first.Call(context.Background(), "new")
	if err != nil {
		t.Fatal(err)
	}
	ref := created[0].GCRef()
	defer first.ReleaseGCRef(ref)
	if _, err := second.CloneGCRefFrom(first, ref); err == nil || !strings.Contains(err.Error(), "distinct Runtime stores") {
		t.Fatalf("same-store clone = %v", err)
	}
	foreignRT, foreignHost, foreignModule, foreign := instantiateForeignCloneFixture(t, cfg, GCConfig{})
	defer foreignRT.Close()
	defer foreignHost.Close()
	defer foreignModule.Close()
	defer foreign.Close()
	if _, err := foreign.CloneGCRefFrom(second, ref); err == nil || !strings.Contains(err.Error(), "source GC reference token") {
		t.Fatalf("wrong source owner clone = %v", err)
	}
}
