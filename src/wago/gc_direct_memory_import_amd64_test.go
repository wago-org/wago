//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"context"
	"strings"
	"testing"
	"unsafe"

	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func directMemoryBytes(mutable byte, startTrap bool) []byte {
	return directMemoryModule(mutable, startTrap, false)
}
func directMemoryModule(mutable byte, startTrap, local bool) []byte {
	types := [][]byte{{0x5f, 1, 0x7f, mutable}, {0x5e, 0x7f, 1},
		{0x60, 1, 0x7f, 1, 0x63, 0}, {0x60, 1, 0x7f, 1, 0x63, 1},
		{0x60, 2, 0x63, 0, 0x7f, 1, 0x7f}, {0x60, 2, 0x63, 1, 0x7f, 1, 0x7f},
		{0x60, 1, 0x7f, 1, 0x7f}, {0x60, 2, 0x7f, 0x7f, 0}, {0x60, 0, 0}}
	bodies := [][]byte{
		{0x20, 0, 0xfb, 0, 0}, {0x20, 0, 0x41, 2, 0xfb, 6, 1},
		{0x20, 1, 0x40, 0, 0x1a, 0xfb, 1, 0, 0x1a, 0x20, 0, 0xfb, 2, 0, 0},
		{0x20, 1, 0x40, 0, 0x1a, 0xfb, 1, 0, 0x1a, 0x20, 0, 0x41, 0, 0xfb, 11, 1},
		{0x20, 0, 0x40, 0}, {0x20, 0, 0x20, 1, 0x36, 2, 0}, {0x20, 0, 0x28, 2, 0}, {0x00},
	}
	sigs := []byte{2, 3, 4, 5, 6, 7, 6, 8}
	names := []string{"new", "array", "step", "arrayStep", "grow", "store", "load", "trap"}
	funcs := make([][]byte, len(sigs))
	exports := [][]byte{wasmtest.ExportEntry("memory", 2, 0)}
	codes := make([][]byte, len(sigs))
	for i := range sigs {
		funcs[i] = []byte{sigs[i]}
		exports = append(exports, wasmtest.ExportEntry(names[i], 0, uint32(i)))
		codes[i] = wasmtest.Code(append(bodies[i], 0x0b))
	}
	imp := append(append(wasmtest.Name("env"), wasmtest.Name("mem")...), 2, 1, 1, 3)
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(2, wasmtest.Vec(imp)), wasmtest.Section(3, wasmtest.Vec(funcs...)), wasmtest.Section(7, wasmtest.Vec(exports...))}
	if local {
		sections = [][]byte{sections[0], sections[2], wasmtest.Section(5, wasmtest.Vec([]byte{1, 1, 3})), sections[3]}
	}
	if startTrap {
		sections = append(sections, wasmtest.Section(8, []byte{7}))
	}
	sections = append(sections, wasmtest.Section(10, wasmtest.Vec(codes...)))
	return wasmtest.Module(sections...)
}

func directMemoryCompile(t *testing.T, mutable byte, trap bool) *Compiled {
	t.Helper()
	c, e := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("dead-gc-new", false).WithOptimization("inline", false), directMemoryBytes(mutable, trap))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { c.Close() })
	if !c.hasGCHelperOnlyReceipt() && c.gcDirectReadABIReceipt() == 0 {
		t.Skip("requires direct/helper compiler artifact")
	}
	return c
}
func directMemoryOpts(rt *Runtime, memory *Memory) InstantiateOptions {
	return InstantiateOptions{store: rt.refStore, Imports: NewImports().Memory("env", "mem", memory), GC: GCConfig{ExperimentalDirectOffsets: true, NurseryBytes: 128, ThroughputHeapBytes: 4096, CollectEveryAlloc: true, ForceMajorEveryMinor: true, VerifyAfterCollect: true}}
}
func directMemoryCheck(t *testing.T, in *Instance) {
	t.Helper()
	if !in.gc.UsesDirectOffsets() {
		t.Fatal("not direct collector")
	}
	if e := in.gc.Verify(gc.EmptyRoots{}); e != nil {
		t.Fatal(e)
	}
	if in.existingPublicGCState() != nil && in.existingPublicGCState().argumentRootCount != 0 {
		t.Fatal("argument roots leaked")
	}
}
func TestDirectGCMemoryImportGrowthAndRoots(t *testing.T) {
	c := directMemoryCompile(t, 0, false)
	rt := NewRuntime()
	defer rt.Close()
	mem, e := NewMemory(1, 3)
	if e != nil {
		t.Fatal(e)
	}
	defer mem.Close()
	in, e := Instantiate(c, directMemoryOpts(rt, mem))
	if e != nil {
		t.Fatal(e)
	}
	defer in.Close()
	for _, kind := range []struct{ new, step string }{{"new", "step"}, {"array", "arrayStep"}} {
		values, e := in.Invoke(kind.new, 73)
		if e != nil {
			t.Fatal(e)
		}
		token := values[0]
		for _, delta := range []uint64{0, 1, 99} {
			entry := in.refStore.gcByToken[token]
			before := in.gc.GlobalSlot(entry.slot)
			count := in.gc.Stats().FullCollections
			out, e := in.Invoke(kind.step, token, delta)
			if e != nil || out[0] != 73 {
				t.Fatalf("%s: %v %v", kind.step, out, e)
			}
			if in.gc.Stats().FullCollections <= count || in.gc.GlobalSlot(entry.slot) == before {
				t.Fatal("no actual callee collection and movement")
			}
			directMemoryCheck(t, in)
		}
		prepared, e := in.WasmFunc(kind.step)
		if e != nil {
			t.Fatal(e)
		}
		out, e := prepared.Invoke(token, 0)
		if e != nil || out[0] != 73 {
			t.Fatalf("prepared %v %v", out, e)
		}
		in.ReleaseGCRef(GCRef{token: token})
	}
	if e := mem.Close(); e == nil {
		t.Fatal("closed backing with live importer")
	}
	out, e := in.Invoke("grow", 99)
	if e != nil || uint32(out[0]) != ^uint32(0) {
		t.Fatalf("failed grow %v %v", out, e)
	}
	out, e = in.Invoke("grow", 0)
	if e != nil || out[0] != 3 {
		t.Fatalf("current pages %v %v", out, e)
	}
	if _, e = in.InvokeValues(context.Background(), "store", ValueI32(65536), ValueI32(99)); e != nil {
		t.Fatal(e)
	}
	out, e = in.Invoke("load", 65536)
	if e != nil || out[0] != 99 {
		t.Fatalf("grown bytes %v %v", out, e)
	}
	directMemoryCheck(t, in)
}

func TestDirectGCMemoryImportRejectsBacking(t *testing.T) {
	c := directMemoryCompile(t, 0, false)
	rt := NewRuntime()
	defer rt.Close()
	shared, e := NewSharedMemory(1, 3)
	if e != nil {
		t.Fatal(e)
	}
	defer shared.Close()
	closed, e := NewMemory(1, 3)
	if e != nil {
		t.Fatal(e)
	}
	closed.Close()
	for _, mem := range []*Memory{nil, {}, shared, closed} {
		in, e := Instantiate(c, directMemoryOpts(rt, mem))
		if e == nil {
			in.Close()
			t.Fatal("accepted unknown/shared/closed memory")
		}
		if !strings.Contains(e.Error(), "known live ordinary memory32 backing") {
			t.Fatalf("wrong rejection: %v", e)
		}
	}
}

func TestDirectGCMemoryImportSharedContextLifetime(t *testing.T) {
	c0 := directMemoryCompile(t, 0, false)
	c1 := directMemoryCompile(t, 1, false)
	host, e := NewMemory(1, 3)
	if e != nil {
		t.Fatal(e)
	}
	defer host.Close()
	r0 := NewRuntime()
	defer r0.Close()
	r1 := NewRuntime()
	defer r1.Close()
	owner, e := Instantiate(c0, directMemoryOpts(r0, host))
	if e != nil {
		t.Fatal(e)
	}
	defer owner.Close()
	memory, e := owner.ExportedMemory("memory")
	if e != nil {
		t.Fatal(e)
	}
	// Re-exporting a host backing permits ordinary ownership sharing without
	// changing it into Wasm thread-shared memory.
	other, e := Instantiate(c1, directMemoryOpts(r1, memory))
	if e != nil {
		t.Fatal(e)
	}
	defer other.Close()
	if owner.gc == other.gc {
		t.Fatal("fixture needs distinct collector domains")
	}
	for n := 0; n < 4; n++ {
		for j, in := range []*Instance{owner, other} {
			value := uint64(40 + j + n)
			token, e := in.Invoke("new", value)
			if e != nil {
				t.Fatal(e)
			}
			refToken := token[0]
			out, e := in.Invoke("step", refToken, 0)
			if e != nil || out[0] != value {
				t.Fatalf("tenant %d %v %v", j, out, e)
			}
			if in.gcDirectView != nil && in.jm.GCNativeViewPtr() != uintptr(unsafe.Pointer(in.gcDirectView)) {
				t.Fatal("wrong rebound direct GC view")
			}
			if e = in.ReleaseGCRef(GCRef{token: refToken}); e != nil {
				t.Fatal(e)
			}
			directMemoryCheck(t, in)
		}
	}
	// Failed start runs after basedata mutation. The previous tenant must still
	// rebind correctly and no failed attachment may survive.
	previousView := other.jm.GCNativeViewPtr()
	badBytes := append(directMemoryBytes(0, false), wasmtest.Section(11, wasmtest.Vec([]byte{0, 0x41, 0x80, 0x80, 0x28, 0x0b, 1, 0}))...)
	badData, e := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), badBytes)
	if e != nil {
		t.Fatal(e)
	}
	defer badData.Close()
	rejected, e := Instantiate(badData, directMemoryOpts(r0, memory))
	if e == nil {
		rejected.Close()
		t.Fatal("out of bounds data initializer succeeded")
	}
	if other.jm.GCNativeViewPtr() != previousView {
		t.Fatal("failed initializer did not restore native GC view")
	}
	bad := directMemoryCompile(t, 0, true)
	failed, e := Instantiate(bad, directMemoryOpts(r0, memory))
	if e == nil {
		failed.Close()
		t.Fatal("trapping start succeeded")
	}
	token, e := other.Invoke("new", 77)
	if e != nil {
		t.Fatal(e)
	}
	refToken := token[0]
	out, e := other.Invoke("step", refToken, 1)
	if e != nil || out[0] != 77 {
		t.Fatalf("after failed construction %v %v", out, e)
	}
	if e = other.ReleaseGCRef(GCRef{token: refToken}); e != nil {
		t.Fatal(e)
	}
	if e = owner.Close(); e != nil {
		t.Fatal(e)
	}
	if _, e = other.Invoke("store", 16, 88); e != nil {
		t.Fatal(e)
	}
	out, e = other.Invoke("load", 16)
	if e != nil || out[0] != 88 {
		t.Fatal(out, e)
	}
	if e = other.Close(); e != nil {
		t.Fatal(e)
	}
	t.Logf("owners roots=%d/%d closed=%v/%v", owner.resourceRefs, other.resourceRefs, owner.resourcesClosed, other.resourcesClosed)
	if e = host.Close(); e != nil {
		t.Fatalf("attachment leaked: %v", e)
	}
}

func TestDirectGCMemoryImportRetainsProducer(t *testing.T) {
	consumerCode := directMemoryCompile(t, 0, false)
	producerCode, e := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("dead-gc-new", false), directMemoryModule(1, false, true))
	if e != nil {
		t.Fatal(e)
	}
	defer producerCode.Close()
	rt := NewRuntime()
	defer rt.Close()
	opts := directMemoryOpts(rt, nil)
	opts.Imports = nil
	producer, e := Instantiate(producerCode, opts)
	if e != nil {
		t.Fatal(e)
	}
	defer producer.Close()
	mem, e := producer.ExportedMemory("memory")
	if e != nil {
		t.Fatal(e)
	}
	consumer, e := Instantiate(consumerCode, directMemoryOpts(rt, mem))
	if e != nil {
		t.Fatal(e)
	}
	defer consumer.Close()
	producer.Close()
	if producer.resourcesClosed {
		t.Fatal("producer freed while imported memory retained")
	}
	rejected, e := Instantiate(consumerCode, directMemoryOpts(rt, mem))
	if e == nil {
		rejected.Close()
		t.Fatal("attached logically closed producer")
	}
	token, e := consumer.Invoke("array", 92)
	if e != nil {
		t.Fatal(e)
	}
	refToken := token[0]
	out, e := consumer.Invoke("arrayStep", refToken, 1)
	if e != nil || out[0] != 92 {
		t.Fatal(out, e)
	}
	if e = consumer.ReleaseGCRef(GCRef{token: refToken}); e != nil {
		t.Fatal(e)
	}
	directMemoryCheck(t, consumer)
	if e = consumer.Close(); e != nil {
		t.Fatal(e)
	}
	t.Logf("producer/consumer roots=%d/%d closed=%v/%v", producer.resourceRefs, consumer.resourceRefs, producer.resourcesClosed, consumer.resourcesClosed)
	if !producer.resourcesClosed {
		t.Fatal("producer retained after final importer close")
	}
}

func TestDirectMemoryImportPolicy(t *testing.T) {
	memory, e := NewMemory(1, 3)
	if e != nil {
		t.Fatal(e)
	}
	defer memory.Close()
	imports, _ := NewImports().Memory("env", "mem", memory).snapshot()
	ordinary := memoryDef{ImportKey: "env.mem", Min: 1, Max: 3, HasMax: true}
	for _, tc := range []struct {
		name   string
		defs   []memoryDef
		accept bool
	}{
		{"ordinary", []memoryDef{ordinary}, true},
		{"memory64", []memoryDef{{ImportKey: "env.mem", Addr64: true}}, false},
		{"threaded", []memoryDef{{ImportKey: "env.mem", Shared: true}}, false},
		{"multiple", []memoryDef{ordinary, ordinary}, false},
		{"importPlusLocal", []memoryDef{ordinary, {}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &Compiled{memoryImport: "env.mem", memoryDir: &compiledMemoryDirectory{defs: tc.defs}}
			b := instanceBuilder{c: c, imports: imports}
			e := b.admitDirectMemoryImport()
			if (e == nil) != tc.accept {
				t.Fatalf("admission=%v accept=%v", e, tc.accept)
			}
		})
	}
}
