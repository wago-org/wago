//go:build linux && amd64 && wago_gc_helper_only

package wago

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

// This rewrites real parked native slots and then resumes that activation. The
// old object remains valid but has a different field, so stale-register reuse
// returns the wrong value rather than being hidden by a liveness-only assertion.
func TestGCHelperOnlyResumesRewrittenRoot(t *testing.T) {
	for _, mode := range []int{0, 1, 2} {
		data := gcHostReentryFrameRootModule()
		want := uint64(777)
		if mode != 0 {
			data = helperOnlyNestedRewriteModule(mode == 2)
			want = 1554
			if mode == 2 {
				want = 2331
			}
		}
		c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("inline", false), data)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		for _, cfg := range []GCConfig{{CollectEveryAlloc: true, VerifyAfterCollect: true}, {Profile: GCProfileTiny, TinyCollectEveryAlloc: true, VerifyAfterCollect: true}} {
			rt := NewRuntime()
			var in *Instance
			rewritten := 0
			in, err = Instantiate(c, InstantiateOptions{GC: cfg, store: rt.refStore, Imports: testImports("env.reenter", slotHostFunc(func(_ HostModule, _, results []uint64) {
				state := in.publicGCState()
				state.mu.Lock()
				defer state.mu.Unlock()
				roots := gcNativeFrameRoots{owner: in, suspended: state, frameLayout: gcNativeFrameLayoutAMD64, allowExternalReturn: true}
				replacement, e := in.gc.NewStructDefaultWithRoots(in.requireGCDomainType(0), &roots)
				if e != nil {
					panic(e)
				}
				if e = in.gc.StructSet(replacement, 0, gc.Value{Kind: gc.StorageI32, Bits: 777}); e != nil {
					panic(e)
				}
				roots.RangeRoots(func(slot gc.RootSlot) bool {
					if slot.GetRef().IsObj() {
						slot.SetRef(replacement)
						rewritten++
					}
					return true
				})
				results[0] = 0
			}))})
			if err != nil {
				t.Fatal(err)
			}
			got, err := in.Invoke("outer", 91)
			if err != nil || len(got) != 1 || got[0] != want || rewritten == 0 {
				t.Fatalf("resumed=%v err=%v rewritten=%d", got, err, rewritten)
			}
			in.Close()
			rt.Close()
		}
	}
}

func helperOnlyNestedRewriteModule(withGlobal bool) []byte {
	st := []byte{0x5f, 1, 0x7f, 1}
	imp := append(append(wasmtest.Name("env"), wasmtest.Name("reenter")...), 0, 1)
	// Keep both a local and a separate operand-stack alias in the caller while
	// the inner function parks in Go. Both must be read from rewritten slots.
	outer := []byte{1, 1, 0x63, 0, 0x20, 0, 0xfb, 0, 0, 0x21, 1}
	if withGlobal {
		outer = append(outer, 0x20, 1, 0x24, 0)
	}
	outer = append(outer, 0x20, 1, 0x10, 2, 0x1a, 0xfb, 2, 0, 0, 0x20, 1, 0xfb, 2, 0, 0, 0x6a)
	if withGlobal {
		outer = append(outer, 0x23, 0, 0xfb, 2, 0, 0, 0x6a)
	}
	outer = append(outer, 0x0b)
	inner := []byte{0, 0x10, 0, 0x0b}
	body := func(b []byte) []byte { return append(wasmtest.ULEB(uint32(len(b))), b...) }
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(st, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))), wasmtest.Section(2, wasmtest.Vec(imp)), wasmtest.Section(3, wasmtest.Vec([]byte{2}, []byte{1})), wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("outer", 0, 1))), wasmtest.Section(10, wasmtest.Vec(body(outer), body(inner))))
}
