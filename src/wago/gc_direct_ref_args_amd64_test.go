//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"context"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func directRefArgsBytes() []byte {
	types := [][]byte{
		{0x5f, 1, 0x7f, 0},                   // scalar struct
		{0x5e, 0x7f, 1},                      // scalar array
		{0x60, 1, 0x7f, 1, 0x63, 0},          // new
		{0x60, 1, 0x63, 0, 1, 0x7f},          // nullable read
		{0x60, 1, 0x64, 0, 1, 0x7f},          // nonnull read
		{0x60, 2, 0x63, 0, 0x63, 0, 1, 0x7f}, // duplicate refs
		{0x60, 1, 0x7f, 1, 0x63, 1},          // new array
		{0x60, 1, 0x63, 1, 1, 0x7f},          // array read
		{0x60, 1, 0x63, 0x6c, 1, 0x7f},       // i31
	}
	alloc := []byte{0xfb, 1, 0, 0x1a}
	read := append(append([]byte{}, alloc...), 0x20, 0, 0xfb, 2, 0, 0)
	bodies := [][]byte{
		{0x20, 0, 0xfb, 0, 0}, read, read,
		append(append([]byte{}, alloc...), 0x20, 0, 0xfb, 2, 0, 0, 0x20, 1, 0xfb, 2, 0, 0, 0x6a),
		{0x20, 0, 0x41, 2, 0xfb, 6, 1},
		append(append([]byte{}, alloc...), 0x20, 0, 0x41, 0, 0xfb, 11, 1),
		append(append([]byte{}, alloc...), 0x20, 0, 0xfb, 0x1d), // i31.get_s
		{0x00, 0x41, 0}, // trap
		{0x41, 0, 0x41, 0xc0, 0x84, 0x3d, 0xfb, 6, 1, 0x1a, 0x20, 0, 0xfb, 2, 0, 0}, // OOM
	}
	bodies = append(bodies, []byte{0x20, 0, 0xd1}, []byte{0x20, 0, 0x10, 1}, []byte{0x42, 1, 0xfb, 0, 9})
	sig := []byte{2, 3, 4, 5, 6, 7, 8, 3, 3, 3, 3, 10}
	names := []string{"new", "read", "nonnull", "pair", "newArray", "readArray", "i31", "trap", "oom", "isNull", "call", "newOther"}
	funcs := make([][]byte, len(sig))
	exports := make([][]byte, len(sig))
	codes := make([][]byte, len(sig))
	for i := range sig {
		funcs[i] = []byte{sig[i]}
		exports[i] = wasmtest.ExportEntry(names[i], 0, uint32(i))
		codes[i] = wasmtest.Code(append(bodies[i], 0x0b))
	}
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(3, wasmtest.Vec(funcs...)), wasmtest.Section(7, wasmtest.Vec(exports...)), wasmtest.Section(10, wasmtest.Vec(codes...)))
}
func TestDirectGCManagedReferenceArguments(t *testing.T) {
	c, e := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("dead-gc-new", false).WithOptimization("inline", false), directRefArgsBytes())
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	// Default handle artifacts are deliberately not admitted to the offset ABI.
	if !c.hasGCHelperOnlyReceipt() && c.gcDirectReadABIReceipt() == 0 {
		t.Skip("requires direct/helper compiler artifact")
	}
	rt := NewRuntime()
	defer rt.Close()
	makeIn := func() *Instance {
		in, e := Instantiate(c, InstantiateOptions{store: rt.refStore, GC: GCConfig{ExperimentalDirectOffsets: true, CollectEveryAlloc: true, ForceMajorEveryMinor: true, VerifyAfterCollect: true, NurseryBytes: 128, ThroughputHeapBytes: 4096}})
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { in.Close() })
		return in
	}
	in := makeIn()
	other := makeIn()
	if !in.gc.UsesDirectOffsets() {
		t.Fatal("wrong collector")
	}
	result, e := in.InvokeValues(context.Background(), "new", ValueI32(91))
	if e != nil {
		t.Fatal(e)
	}
	token := result[0].GCRef()
	defer in.ReleaseGCRef(token)
	entry := in.refStore.gcByToken[token.token]
	before := in.gc.GlobalSlot(entry.slot)
	if e = in.gc.CollectFull(gc.EmptyRoots{}); e != nil {
		t.Fatal(e)
	}
	if in.gc.GlobalSlot(entry.slot) == before {
		t.Fatal("no relocation")
	}
	check := func() {
		t.Helper()
		if e := in.gc.Verify(gc.EmptyRoots{}); e != nil {
			t.Fatal(e)
		}
		if st := in.existingPublicGCState(); st != nil && st.argumentRootCount != 0 {
			t.Fatal("argument roots leaked")
		}
	}
	nullResult, err := in.Invoke("isNull", 0)
	if err != nil || nullResult[0] != 1 {
		t.Fatal(nullResult, err)
	}
	check()
	for i := 0; i < 8; i++ {
		collections := in.gc.Stats().FullCollections
		oldRef := in.gc.GlobalSlot(entry.slot)
		got, e := in.Invoke("read", token.token)
		if e != nil || got[0] != 91 {
			t.Fatal(got, e)
		}
		check()
		if in.gc.Stats().FullCollections <= collections || in.gc.GlobalSlot(entry.slot) == oldRef {
			t.Fatal("callee did not move the argument")
		}
		got, e = in.Invoke("call", token.token)
		if e != nil || got[0] != 91 {
			t.Fatal(got, e)
		}
		check()

		typed, e := in.InvokeValues(context.Background(), "nonnull", result[0])
		if e != nil || typed[0].Bits() != 91 {
			t.Fatal(typed, e)
		}
		check()
		fn, e := in.WasmFunc("pair")
		if e != nil {
			t.Fatal(e)
		}
		got, e = fn.Invoke(token.token, token.token)
		if e != nil || got[0] != 182 {
			t.Fatal(got, e)
		}
		check()
	}
	different, e := in.InvokeValues(context.Background(), "newOther")
	if e != nil {
		t.Fatal(e)
	}
	defer in.ReleaseGCRef(different[0].GCRef())
	if _, e = in.Invoke("read", different[0].Bits()); e == nil {
		t.Fatal("wrong indexed struct type accepted")
	}
	check()
	if _, e = in.Invoke("i31", token.token); e == nil {
		t.Fatal("object accepted as i31")
	}
	check()
	array, e := in.InvokeValues(context.Background(), "newArray", ValueI32(37))
	if e != nil {
		t.Fatal(e)
	}
	defer in.ReleaseGCRef(array[0].GCRef())
	got, e := in.Invoke("readArray", array[0].Bits())
	if e != nil || got[0] != 37 {
		t.Fatal(got, e)
	}
	check()
	for _, args := range []struct {
		name string
		args []uint64
	}{
		{"nonnull", []uint64{0}}, {"read", []uint64{24}}, {"read", []uint64{array[0].Bits()}},
		{"pair", []uint64{token.token, 24}}, {"read", []uint64{uint64(gc.I31New(17))}}, {"read", []uint64{0}}, {"trap", []uint64{token.token}}, {"oom", []uint64{token.token}},
	} {
		if _, e = in.Invoke(args.name, args.args...); e == nil {
			t.Fatal("expected failure", args.name)
		}
		check()
	}
	if _, e = other.Invoke("read", token.token); e == nil {
		t.Fatal("other-instance token admitted")
	}
	prepared, e := in.WasmFunc("pair")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = prepared.Invoke(token.token, 24); e == nil {
		t.Fatal("prepared bad token accepted")
	}
	check()
	foreignRT := NewRuntime()
	defer foreignRT.Close()
	foreign, e := Instantiate(c, InstantiateOptions{store: foreignRT.refStore, GC: GCConfig{ExperimentalDirectOffsets: true, NurseryBytes: 128, ThroughputHeapBytes: 4096}})
	if e != nil {
		t.Fatal(e)
	}
	defer foreign.Close()
	if _, e = foreign.Invoke("read", token.token); e == nil {
		t.Fatal("foreign runtime token accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e = in.InvokeValues(ctx, "read", result[0]); e == nil {
		t.Fatal("canceled call accepted")
	}
	check()
	got, e = in.Invoke("i31", uint64(gc.I31New(17)))
	if e != nil || got[0] != 17 {
		t.Fatal(got, e)
	}
	check()
	stale, e := in.InvokeValues(context.Background(), "new", ValueI32(1))
	if e != nil {
		t.Fatal(e)
	}
	in.ReleaseGCRef(stale[0].GCRef())
	if _, e = in.Invoke("read", stale[0].Bits()); e == nil {
		t.Fatal("released token accepted")
	}
	check()
	got, e = in.Invoke("read", token.token)
	if e != nil || got[0] != 91 {
		t.Fatal("retry", got, e)
	}
}
