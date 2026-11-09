package wago

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Declared-local kinds cycled through the frame: i64, i32, f64, f32, v128,
// funcref, anyref.
var bulkLocalZeroKinds = []byte{0x7e, 0x7f, 0x7c, 0x7d, 0x7b, 0x70, 0x6e}

func bulkLocalZeroCode(locals int, body []byte) []byte {
	decls := [][]byte{}
	for i := 0; i < locals; i++ {
		decls = append(decls, append(wasmtest.ULEB(1), bulkLocalZeroKinds[i%len(bulkLocalZeroKinds)]))
	}
	fn := append(wasmtest.Vec(decls...), body...)
	return append(wasmtest.ULEB(uint32(len(fn))), fn...)
}

// bulkLocalZeroWrites stores a nonzero value derived from param 0 into each local.
func bulkLocalZeroWrites(locals int) []byte {
	var b []byte
	for i := 0; i < locals; i++ {
		local := wasmtest.ULEB(uint32(i + 1))
		k := int64(i + 1)
		switch bulkLocalZeroKinds[i%len(bulkLocalZeroKinds)] {
		case 0x7e:
			b = append(b, 0x20, 0, 0x42)
			b = append(append(b, wasmtest.SLEB64(k)...), 0x7c)
		case 0x7f:
			b = append(b, 0x20, 0, 0xa7, 0x41)
			b = append(append(b, wasmtest.SLEB32(int32(k))...), 0x6a)
		case 0x7c:
			b = append(b, 0x20, 0, 0xb9, 0x44, 0, 0, 0, 0, 0, 0, 0xf8, 0x3f, 0xa0) // + 1.5
		case 0x7d:
			b = append(b, 0x20, 0, 0xb4, 0x43, 0, 0, 0xc0, 0x3f, 0x92) // + 1.5
		case 0x7b:
			b = append(b, 0x20, 0, 0x42)
			b = append(append(b, wasmtest.SLEB64(k)...), 0x7c, 0xfd)
			b = append(b, wasmtest.ULEB(18)...) // i64x2.splat
		case 0x70:
			b = append(b, 0xd2, 0) // ref.func 0
		case 0x6e:
			b = append(b, 0xd2, 0, 0xfb, 0, 0) // struct.new 0 (ref.func 0)
		}
		b = append(append(b, 0x21), local...)
	}
	return b
}

// bulkLocalZeroReads ORs every local's bits into one i64 (funcref: non-null is 1).
func bulkLocalZeroReads(locals int) []byte {
	b := []byte{0x42, 0}
	for i := 0; i < locals; i++ {
		b = append(append(b, 0x20), wasmtest.ULEB(uint32(i+1))...)
		switch bulkLocalZeroKinds[i%len(bulkLocalZeroKinds)] {
		case 0x7f:
			b = append(b, 0xad) // i64.extend_i32_u
		case 0x7c:
			b = append(b, 0xbd) // i64.reinterpret_f64
		case 0x7d:
			b = append(b, 0xbc, 0xad) // i32.reinterpret_f32
		case 0x7b:
			b = append(b, 0xfd)
			b = append(append(b, wasmtest.ULEB(83)...), 0xad) // v128.any_true
		case 0x70, 0x6e:
			b = append(b, 0xd1, 0x45, 0xad) // ref.is_null; i32.eqz
		}
		b = append(b, 0x84) // i64.or
	}
	return b
}

// bulkLocalZeroModule: dirty fills a frame, probe runs at the same depth and
// must read every never-before-written local as zero, run calls them in turn.
// Struct allocations give both frames exact GC root maps.
func bulkLocalZeroModule(locals int) []byte {
	dirty := append(bulkLocalZeroWrites(locals), 0x03, 0x40, 0x0b)
	dirty = append(append(dirty, bulkLocalZeroReads(locals)...), 0x0b)
	probe := append([]byte{0x03, 0x40, 0x0b}, bulkLocalZeroReads(locals)...)
	// Writes after the reads keep every local live and assigned in the body.
	probe = append(probe, 0x20, 0, 0x42)
	probe = append(append(probe, wasmtest.SLEB64(12345)...), 0x51, 0x04, 0x40)
	probe = append(append(probe, bulkLocalZeroWrites(locals)...), 0x0b, 0x0b)
	run := []byte{0x42, 7, 0x10, 0, 0x1a, 0x42, 0, 0x10, 1, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x70, 1}, []byte{0x60, 1, 0x7e, 1, 0x7e}, []byte{0x60, 0, 1, 0x7e})),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{1}, []byte{2})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("dirty", 0, 0), wasmtest.ExportEntry("run", 0, 2))),
		wasmtest.Section(9, wasmtest.Vec([]byte{3, 0, 1, 0})), // declare func 0 for ref.func
		wasmtest.Section(10, wasmtest.Vec(bulkLocalZeroCode(locals, dirty), bulkLocalZeroCode(locals, probe), wasmtest.Code(run))),
	)
}

// Entry zeroing must clear every declared local whatever the frame size and
// type mix, including runs long enough to be cleared by a loop.
func TestDeclaredLocalsReadZeroOverDirtyFrame(t *testing.T) {
	if !platformCoreFeatures().IsEnabled(CoreFeaturesV3) {
		t.Skip("Core 3 execution is unavailable on this backend")
	}
	for _, locals := range []int{8, 31, 32, 33, 47, 64, 65, 130, 301, 1030} {
		t.Run(fmt.Sprintf("locals=%d", locals), func(t *testing.T) {
			c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), bulkLocalZeroModule(locals))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{NurseryBytes: 4096, SurvivorBytes: 4096}})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			dirty, err := in.Invoke("dirty", 7)
			if err != nil {
				t.Fatal(err)
			}
			if dirty[0] == 0 {
				t.Fatal("dirty frame wrote only zero values")
			}
			for i := 0; i < 3; i++ {
				got, err := in.Invoke("run")
				if err != nil {
					t.Fatal(err)
				}
				if got[0] != 0 {
					t.Fatalf("run %d: never-written locals read %#x, want 0", i, got[0])
				}
			}
		})
	}
}
