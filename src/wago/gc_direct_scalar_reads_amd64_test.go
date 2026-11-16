//go:build linux && amd64 && (wago_gc_direct_reads || wago_gc_direct_alloc || wago_gc_direct_refstore || wago_gc_direct_trusted || wago_gc_direct_trusted_store || wago_gc_direct_scalar_arrays || wago_gc_direct_trusted_arrays || wago_gc_direct_array_alloc || wago_gc_direct_trusted_refstruct || wago_gc_direct_trusted_alloc)

package wago

import (
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"math"
	"testing"
)

func TestDirectGCScalarReadsAfterRelocation(t *testing.T) {
	for _, tc := range []struct {
		name               string
		storage, param, op byte
		arg, want          uint64
	}{
		{"i8-s", 0x78, 0x7f, 3, 255, 0xffffffff}, {"i8-u", 0x78, 0x7f, 4, 255, 255},
		{"i16-s", 0x77, 0x7f, 3, 65535, 0xffffffff}, {"i16-u", 0x77, 0x7f, 4, 65535, 65535},
		{"i32", 0x7f, 0x7f, 2, 0xdeadbeef, 0xdeadbeef}, {"i64", 0x7e, 0x7e, 2, 0x123456789abcdef, 0x123456789abcdef},
		{"f32", 0x7d, 0x7d, 2, uint64(math.Float32bits(-1.25)), uint64(math.Float32bits(-1.25))},
		{"f64", 0x7c, 0x7c, 2, math.Float64bits(-1.25), math.Float64bits(-1.25)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte{1, 1, 0x63, 0, 0x20, 0, 0xfb, 0, 0, 0x21, 1, 0xfb, 1, 0, 0x1a, 0x20, 1, 0xfb, tc.op, 0, 0, 0x0b}
			data := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, tc.storage, 0}, []byte{0x60, 1, tc.param, 1, tc.param})), wasmtest.Section(3, wasmtest.Vec([]byte{1})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))))
			c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			if c.gcDirectReadABIReceipt() != gc.DirectABIVersion || c.hasGCHelperOnlyReceipt() {
				t.Fatal("wrong artifact receipt")
			}
			rt := NewRuntime()
			defer rt.Close()
			in, err := Instantiate(c, InstantiateOptions{store: rt.refStore, GC: GCConfig{ExperimentalDirectOffsets: true, CollectEveryAlloc: true, ForceMajorEveryMinor: true, NurseryBytes: 128, ThroughputHeapBytes: 64 << 10}})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			for i := 0; i < 8; i++ {
				got, err := in.Invoke("run", tc.arg)
				if err != nil || len(got) != 1 || got[0] != tc.want {
					t.Fatalf("got%v err%v want%x", got, err, tc.want)
				}
			}
			if in.gc.Stats().FullCollections == 0 {
				t.Fatal("no major collection")
			}
		})
	}
}

func TestDirectGCArrayLengthAfterRelocation(t *testing.T) {
	// The private callee has a typed array parameter. Its receiver stays live
	// while another allocation forces relocation before native array.len.
	body := []byte{0xfb, 1, 1, 0x1a, 0x20, 0, 0xfb, 15, 0x0b}
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5e, 0x7e, 1}, []byte{0x5f, 0}, []byte{0x60, 1, 0x63, 0, 1, 0x7f}, []byte{0x60, 0, 1, 0x7f})),
		wasmtest.Section(3, wasmtest.Vec([]byte{2}, []byte{3})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body), wasmtest.Code([]byte{0x41, 7, 0xfb, 7, 0, 0x10, 0, 0x0b}))),
	)
	c, e := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("inline", false), data)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	rt := NewRuntime()
	defer rt.Close()
	in, e := Instantiate(c, InstantiateOptions{store: rt.refStore, GC: GCConfig{ExperimentalDirectOffsets: true, CollectEveryAlloc: true, ForceMajorEveryMinor: true, NurseryBytes: 256, ThroughputHeapBytes: 64 << 10}})
	if e != nil {
		t.Fatal(e)
	}
	defer in.Close()
	for i := 0; i < 8; i++ {
		got, e := in.Invoke("run")
		if e != nil || len(got) != 1 || got[0] != 7 {
			t.Fatal(got, e)
		}
	}
}
