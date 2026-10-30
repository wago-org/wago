//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

// Portable accounting/benchmark harness: this file alone can be copied to
// 6ceb86221. The three standard expression bodies are byte-for-byte Stage 1's
// lifetimeFixture. Only an export is added to invoke them. No Stage 2 API needed.
import (
	"encoding/binary"
	"fmt"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func lifetimeRuntimeModule(kind string, scalar wasm.ValType, params bool) []byte {
	body := []byte{0x41, 42, 0xfb, 0, 0, 0x21, 0, 0x20, 0, 0x22, 1, 0x1a, 0x01, 0x20, 1, 0xfb, 2, 0, 0, 0x0b}
	if kind == "nonqualifying" {
		body = []byte{0x41, 42, 0xfb, 0, 0, 0x21, 0, 0x02, 0x40, 0x01, 0x0b, 0x20, 0, 0xfb, 2, 0, 0, 0x0b}
	}
	if kind == "no-gc" {
		body = []byte{0x41, 42, 0x0b}
	}
	var pt []wasm.ValType
	if params {
		pt = []wasm.ValType{scalar}
		body = []byte{0x20, 0, 0xfb, 0, 0, 0x21, 1, 0x20, 1, 0x22, 2, 0x1a, 0x01, 0x20, 2, 0xfb, 2, 0, 0, 0x0b}
	}
	if kind == "default" {
		body = []byte{0xfb, 1, 0, 0x21, 0, 0x20, 0, 0xfb, 2, 0, 0, 0x0b}
	}
	if kind == "overwrite" {
		body = []byte{0x41, 42, 0xfb, 0, 0, 0x22, 0, 0x21, 1, 0x20, 1, 0x21, 0, 0x20, 0, 0xfb, 2, 0, 0, 0x0b}
	}
	tag := byte(0x7f)
	switch scalar {
	case wasm.I64:
		tag = 0x7e
	case wasm.F32:
		tag = 0x7d
	case wasm.F64:
		tag = 0x7c
	}
	decls := []byte{1, 2, 0x63, 0}
	if kind == "scalar-overwrite" {
		// Save an unwritten parameter through a scalar local, initialize the object,
		// then overwrite that local before reading through a reference alias.
		body = []byte{0x20, 0, 0x21, 3, 0x20, 3, 0xfb, 0, 0, 0x21, 1, 0x41, 0xe3, 0, 0x21, 3, 0x20, 1, 0xfb, 2, 0, 0, 0x0b}
		decls = []byte{2, 2, 0x63, 0, 1, tag}
	}
	code := append(decls, body...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, tag, 0}, wasmtest.FuncType(pt, []wasm.ValType{scalar}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(code))), code...))))
}

type lifetimeCounters struct {
	allocations, cycle uint64
	dead, bump         uint32
	deadCycle          uint64
}

func lifetimeSnapshot(i *Instance) lifetimeCounters {
	s := i.gc.Stats()
	v := i.gc.NativeView()
	out := lifetimeCounters{allocations: s.Allocations, cycle: s.MinorCollections + s.FullCollections, deadCycle: v.DeadNurseryCycle}
	if v.AllocationCount != 0 {
		p := unsafe.Slice((*byte)(offHeapPtr(v.AllocationCount)), gc.NativeAllocationDeadObjectsOffset+4)
		out.dead = binary.LittleEndian.Uint32(p[gc.NativeAllocationDeadObjectsOffset:])
	}
	if v.NurseryBump != 0 {
		out.bump = *(*uint32)(offHeapPtr(v.NurseryBump))
	}
	return out
}
func lifetimeHandleless(a, b lifetimeCounters) uint64 {
	if b.deadCycle != b.cycle {
		return 0
	}
	if a.deadCycle == b.deadCycle && b.dead >= a.dead {
		return uint64(b.dead - a.dead)
	}
	return uint64(b.dead)
}
func lifetimeInstance(tb testing.TB, data []byte, on bool, cfg GCConfig) *Instance {
	tb.Helper()
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("gc-lifetime-analyze", on), data)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { c.Close() })
	i, err := Instantiate(c, InstantiateOptions{GC: cfg})
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { i.Close() })
	return i
}

func TestGCLifetimeStage2Runtime(t *testing.T) {
	modes := []struct {
		name string
		cfg  GCConfig
	}{
		{"throughput", GCConfig{}}, {"small", GCConfig{NurseryBytes: 1024, SurvivorBytes: 1024}},
		{"stress", GCConfig{CollectEveryAlloc: true}}, {"disabled", GCConfig{DisableCollection: true, ThroughputHeapBytes: 65536}},
		{"tiny", GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 65536}},
		{"tiny-stress", GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 65536, TinyCollectEveryAlloc: true}},
	}
	for _, mode := range modes {
		for _, kind := range []string{"qualifying", "nonqualifying", "no-gc", "default", "overwrite"} {
			t.Run(mode.name+"/"+kind, func(t *testing.T) {
				for _, on := range []bool{false, true} {
					i := lifetimeInstance(t, lifetimeRuntimeModule(kind, wasm.I32, false), on, mode.cfg)
					var charges, handleless uint64
					for n := 0; n < 128; n++ {
						a := lifetimeSnapshot(i)
						got, err := i.Invoke("run")
						b := lifetimeSnapshot(i)
						want := uint64(42)
						if kind == "default" {
							want = 0
						}
						if err != nil || len(got) != 1 || got[0] != want {
							t.Fatalf("on=%v: %v %v", on, got, err)
						}
						charges += b.allocations - a.allocations
						handleless += lifetimeHandleless(a, b)
					}
					want := uint64(128)
					if kind == "no-gc" {
						want = 0
					}
					if charges != want {
						t.Fatalf("charges %d want %d", charges, want)
					}
					t.Logf("on=%v charges=%d layout-bytes=%d handleless=%d materialized=%d", on, charges, charges*24, handleless, charges-handleless)
					if on && kind == "qualifying" && mode.name == "throughput" && gc.NativeDeadBatchSupported && nativeStructAllocEnabled && handleless == 0 {
						t.Fatal("no allocation publication eliminated")
					}
					if (!on || kind == "nonqualifying" || mode.name == "tiny" || mode.name == "tiny-stress" || mode.name == "stress" || mode.name == "disabled") && handleless != 0 {
						t.Fatal("unexpected handleless path")
					}
				}
			})
		}
	}
}

func TestGCLifetimeStage2ScalarBits(t *testing.T) {
	for _, typ := range []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64} {
		for _, bits := range []uint64{0, 1, 0x80000000, 0xffc01234, 0x8000000000000000, 0xfff8000012345678} {
			for _, on := range []bool{false, true} {
				t.Run(fmt.Sprintf("%v/%x/%v", typ, bits, on), func(t *testing.T) {
					i := lifetimeInstance(t, lifetimeRuntimeModule("qualifying", typ, true), on, GCConfig{NurseryBytes: 1024, SurvivorBytes: 1024})
					want := bits
					if typ == wasm.I32 || typ == wasm.F32 {
						want = uint64(uint32(bits))
					}
					for n := 0; n < 96; n++ {
						got, err := i.Invoke("run", bits)
						if err != nil || len(got) != 1 || got[0] != want {
							t.Fatalf("%x %v want %x", got, err, want)
						}
					}
				})
			}
		}
	}
}

func TestGCLifetimeStage2Exhaustion(t *testing.T) {
	for _, cfg := range []GCConfig{

		{Profile: GCProfileTiny, TinyHeapBytes: 16, TinyBlockBytes: 16},
	} {
		var counts [2]int
		var traps [2]string
		for k, on := range []bool{false, true} {
			i := lifetimeInstance(t, lifetimeRuntimeModule("qualifying", wasm.I32, false), on, cfg)
			for ; counts[k] < 10000; counts[k]++ {
				_, err := i.Invoke("run")
				if err != nil {
					traps[k] = err.Error()
					break
				}
			}
		}
		if counts[0] != counts[1] || counts[0] == 10000 || traps[0] != traps[1] {
			t.Fatalf("exhaustion changed: counts=%v traps=%v", counts, traps)
		}
		t.Logf("config=%+v off/on trap after %d: %s", cfg, counts[0], traps[0])
	}
}

func BenchmarkGCLifetimeRuntime(b *testing.B) {
	for _, mode := range []struct {
		name string
		cfg  GCConfig
	}{{"throughput", GCConfig{}}, {"tiny", GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 65536}}} {
		for _, kind := range []string{"qualifying", "nonqualifying", "no-gc"} {
			for _, on := range []bool{false, true} {
				b.Run(fmt.Sprintf("%s/%s/on=%v", mode.name, kind, on), func(b *testing.B) {
					i := lifetimeInstance(b, lifetimeRuntimeModule(kind, wasm.I32, false), on, mode.cfg)
					b.ResetTimer()
					for n := 0; n < b.N; n++ {
						got, err := i.Invoke("run")
						if err != nil || len(got) != 1 || got[0] != 42 {
							b.Fatal(got, err)
						}
					}
					b.StopTimer()
					// Separate fresh-instance accounting, excluded from timing. The native dead
					// counter is cycle-local; sample every invocation to include reset/refills.
					j := lifetimeInstance(b, lifetimeRuntimeModule(kind, wasm.I32, false), on, mode.cfg)
					var charged, dead, nurseryAdvance uint64
					for n := 0; n < 1024; n++ {
						a := lifetimeSnapshot(j)
						_, err := j.Invoke("run")
						if err != nil {
							b.Fatal(err)
						}
						z := lifetimeSnapshot(j)
						charged += z.allocations - a.allocations
						if z.cycle != a.cycle {
							nurseryAdvance += uint64(z.bump)
						} else if z.bump > a.bump {
							nurseryAdvance += uint64(z.bump - a.bump)
						}
						dead += lifetimeHandleless(a, z)
					}
					b.ReportMetric(float64(charged)/1024, "guest-charges/op")
					b.ReportMetric(float64(nurseryAdvance)/1024, "observed-nursery-advance-B/op")
					// Size-class rounding for throughput heap allocations, not a
					// measurement of nursery/chunk reservation traffic.
					b.ReportMetric(float64(charged*32)/1024, "throughput-rounded-B/op")
					b.ReportMetric(float64(charged*24)/1024, "layout-B/op")
					b.ReportMetric(float64(dead)/1024, "handleless/op")
					b.ReportMetric(float64(charged-dead)/1024, "materialized/op")
					b.ReportMetric(float64(lifetimeSnapshot(j).bump), "retained-nursery-B")
				})
			}
		}
	}

}

func TestGCLifetimeStage2PersistentRoots(t *testing.T) {
	for _, cfg := range []GCConfig{{StressNurseryBytes: 128, VerifyAfterCollect: true}, {CollectEveryAlloc: true, VerifyAfterCollect: true}, {Profile: GCProfileTiny, TinyHeapBytes: 4096, TinyCollectEveryAlloc: true}} {
		for _, on := range []bool{false, true} {
			i := lifetimeInstance(t, lifetimeRuntimeModule("qualifying", wasm.I32, false), on, cfg)
			ref, err := i.gc.NewStructWithRoots(0, []gc.Value{gc.I32Value(987)}, gc.EmptyRoots{})
			if err != nil {
				t.Fatal(err)
			}
			slot := i.gc.NewGlobalSlot(ref)
			for n := 0; n < 256; n++ {
				got, err := i.Invoke("run")
				if err != nil || len(got) != 1 || got[0] != 42 {
					t.Fatal(got, err)
				}
				root, err := i.gc.CheckedGlobalSlot(slot)
				if err != nil {
					t.Fatal(err)
				}
				v, err := i.gc.StructGet(root, 0)
				if err != nil || v.I32() != 987 {
					t.Fatalf("root lost on=%v n=%d: %v %v", on, n, v, err)
				}
			}
		}
	}
}

func TestGCLifetimeStage2CycleAccounting(t *testing.T) {
	for _, tc := range []struct {
		a, b lifetimeCounters
		want uint64
	}{
		{lifetimeCounters{cycle: 4, deadCycle: 4, dead: 9}, lifetimeCounters{cycle: 4, deadCycle: 4, dead: 10}, 1},
		{lifetimeCounters{cycle: 4, deadCycle: 4, dead: 9}, lifetimeCounters{cycle: 5, deadCycle: 5, dead: 1}, 1},
		{lifetimeCounters{cycle: 4, deadCycle: 4, dead: 9}, lifetimeCounters{cycle: 5, deadCycle: 4, dead: 9}, 0},
		{lifetimeCounters{cycle: 5, deadCycle: 4, dead: 9}, lifetimeCounters{cycle: 5, deadCycle: 5, dead: 1}, 1},
	} {
		if got := lifetimeHandleless(tc.a, tc.b); got != tc.want {
			t.Fatalf("%+v got %d", tc, got)
		}
	}
}

func TestGCLifetimeStage2ScalarOverwrite(t *testing.T) {
	for _, on := range []bool{false, true} {
		i := lifetimeInstance(t, lifetimeRuntimeModule("scalar-overwrite", wasm.I32, true), on, GCConfig{StressNurseryBytes: 128, VerifyAfterCollect: true})
		for n := uint64(0); n < 256; n++ {
			got, err := i.Invoke("run", n)
			if err != nil || len(got) != 1 || got[0] != n {
				t.Fatal(got, err, n)
			}
		}
	}
}
