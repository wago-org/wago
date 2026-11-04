//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	corergc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// The final parent contains a typed child, rather than an abstract eqref. The
// hidden export removes every parent root while its loaded child stays on the
// operand stack across an allocation. The cached export keeps the parent in a
// local, reads it twice, and requires a fresh resolution after collection.
func gcReferenceReadModule() []byte {
	types := [][]byte{
		{0x5f, 0x01, 0x7f, 0x01},             // 0: child (mut i32)
		{0x5f, 0x01, 0x63, 0x00, 0x01},       // 1: parent (mut (ref null child))
		{0x5f, 0x01, 0x6d, 0x01},             // 2: box (mut eqref)
		{0x5f, 0x02, 0x7f, 0x01, 0x7f, 0x01}, // 3: distinct wrong parent
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
	}
	global := []byte{0x63, 0x01, 0x01, 0xd0, 0x01, 0x0b}
	bodies := [][]byte{
		wasmtest.Code([]byte{
			0x41, 0x49, 0xfb, 0x00, 0x00, // child(-55), signed LEB
			0xfb, 0x00, 0x01, 0x24, 0x00, // parent(child); global.set
			0x23, 0x00, 0xfb, 0x02, 0x01, 0x00, // loaded typed child
			0xd0, 0x01, 0x24, 0x00, // parent/global are now unreachable
			0x41, 0x00, 0xfb, 0x00, 0x00, 0x1a, // collecting allocation
			0xfb, 0x02, 0x00, 0x00, 0x0b, // child.i32
		}),
		// One nullable parent local. Repeated reads of that same local can cache
		// its address, but an allocating constructor must revoke that cache.
		append([]byte{0x01, 0x01, 0x63, 0x01}, []byte{
			0x41, 0x2a, 0xfb, 0x00, 0x00, 0xfb, 0x00, 0x01, 0x21, 0x00,
			0x20, 0x00, 0xfb, 0x02, 0x01, 0x00, 0x1a,
			0x20, 0x00, 0xfb, 0x02, 0x01, 0x00, 0x1a,
			0x41, 0x00, 0xfb, 0x00, 0x00, 0x1a,
			0x20, 0x00, 0xfb, 0x02, 0x01, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x0b,
		}...),
		wasmtest.Code([]byte{0xd0, 0x00, 0xfb, 0x00, 0x01, 0xfb, 0x02, 0x01, 0x00, 0xd1, 0x0b}),
		wasmtest.Code([]byte{0x41, 0x79, 0xfb, 0x1c, 0xfb, 0x00, 0x02, 0xfb, 0x02, 0x02, 0x00, 0xfb, 0x16, 0x6c, 0xfb, 0x1d, 0x0b}),
		wasmtest.Code([]byte{0x23, 0x00, 0xfb, 0x02, 0x01, 0x00, 0xd1, 0x0b}),
	}
	// The second body includes its own local declarations and needs a size prefix.
	bodies[1] = append(wasmtest.ULEB(uint32(len(bodies[1]))), bodies[1]...)
	names := []string{"hidden", "cached", "nullable", "i31", "global_read"}
	funcs, exports := make([][]byte, len(bodies)), make([][]byte, len(bodies))
	for i, name := range names {
		funcs[i] = wasmtest.ULEB(4)
		exports[i] = wasmtest.ExportEntry(name, byte(wasm.ExternFunc), uint32(i))
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(funcs...)),
		wasmtest.Section(6, wasmtest.Vec(global)),
		wasmtest.Section(7, wasmtest.Vec(exports...)),
		wasmtest.Section(10, wasmtest.Vec(bodies...)),
	)
}

func TestGCReferenceReadRootsAcrossCollectionAndCodec(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcReferenceReadModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()

	profiles := []struct {
		name string
		cfg  GCConfig
	}{
		{"throughput", GCConfig{StressNurseryBytes: 128, CollectEveryAlloc: true, ForceMajorEveryMinor: true, VerifyAfterCollect: true, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096}},
		{"tiny", GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 256, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true, StressBarriers: true}},
	}
	for _, artifact := range []struct {
		name string
		code *Compiled
	}{{"fresh", compiled}, {"codec", compiled}} {
		for _, profile := range profiles {
			t.Run(artifact.name+"/"+profile.name, func(t *testing.T) {
				in, err := Instantiate(artifact.code, InstantiateOptions{GC: profile.cfg})
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				if in.c.genericGCFrameRoots() == nil || !in.gc.CollectsOnAllocation() {
					t.Fatal("reference read fixture requires exact roots and active collection")
				}
				for range 8 {
					for _, test := range []struct {
						name string
						want uint64
					}{{"hidden", uint64(uint32(0xffffffc9))}, {"cached", 42}, {"nullable", 1}, {"i31", uint64(uint32(0xfffffff9))}} {
						got, err := in.Invoke(test.name)
						if err != nil || len(got) != 1 || got[0] != test.want {
							t.Fatalf("%s = %#x, %v; want [%#x]", test.name, got, err, test.want)
						}
					}
				}
				if bits := readGlobalObject(in.globalCells[0], in.c.Globals[0].Type); bits != 0 {
					t.Fatalf("parent global remains reachable: %#x", bits)
				}
				if stats := in.gc.Stats(); stats.FullCollections == 0 {
					t.Fatalf("active collection did not occur: %+v", stats)
				}
			})
		}
	}
}

// Null and forged/wrong parent references are intentionally invalid. Native
// reads must trap before accessing their payload, including after codec reload.
func TestGCReferenceReadRejectsInvalidParent(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcReferenceReadModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()

	for _, artifact := range []*Compiled{compiled} {
		in, err := Instantiate(artifact, InstantiateOptions{GC: GCConfig{VerifyAfterCollect: true}})
		if err != nil {
			t.Fatal(err)
		}
		wrong, err := in.gc.NewStructDefaultWithRoots(in.requireGCDomainType(3), corergc.EmptyRoots{})
		if err != nil {
			in.Close()
			t.Fatal(err)
		}
		for _, test := range []struct {
			name string
			bits uint64
			code TrapCode
		}{{"null", 0, TrapNullReference}, {"forged", 0xfffffffe, TrapCastFailure}, {"wrong_type", uint64(wrong), TrapCastFailure}, {"i31_parent", uint64(corergc.I31New(1)), TrapCastFailure}} {
			writeGlobalObject(in.globalCells[0], in.c.Globals[0].Type, test.bits)
			_, err := in.Invoke("global_read")
			var trap *TrapError
			if !errors.As(err, &trap) || trap.Code != test.code {
				in.Close()
				t.Fatalf("%s parent = %v; want trap %d", test.name, err, test.code)
			}
		}
		writeGlobalObject(in.globalCells[0], in.c.Globals[0].Type, 0)
		in.Close()
	}
}
