//go:build linux && amd64

package amd64

import (
	"bytes"
	"encoding/binary"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"math"
	"testing"
)

func TestExperimentalMaps(t *testing.T) {
	for _, name := range []string{"map-f32", "map-i32"} {
		t.Run(name, func(t *testing.T) {
			mode := "vector-i32"
			if name == "map-f32" {
				mode = "vector-f32"
			}
			m := loopExperimentModule(t, name)
			lowered, reason, err := shared.RewriteReplication(m, mode)
			if err != nil || reason != "accepted" || lowered == m {
				t.Fatal(reason, err)
			}
			ref := newLoopExperimentRun(t, m, CompileOptions{AMD64FeaturesSet: true}, 65536)
			got := newLoopExperimentRun(t, m, CompileOptions{AMD64FeaturesSet: true, ExperimentalLoopMode: mode}, 65536)
			fast := newLoopExperimentRun(t, m, CompileOptions{AMD64FeaturesSet: true, ExperimentalLoopMode: mode + "-assert"}, 65536)
			for _, n := range []uint64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 15, 16, 17, 31, 32, 33, 511, 512, 513, 8192} {
				for _, pair := range [][2]uint64{{128, 32768}, {128, 128}, {129, 128}, {128, 129}, {132, 128}, {128, 132}, {1, 32769}, {65520, 128}, {128, 65520}} {
					init := func(mem []byte) {
						for i := range mem {
							mem[i] = 0xa5
						}
						values := []uint32{0, 0x80000000, 1, 0x7f800000, 0xff800000, 0x7fc12345, 0x3f800001, 0x3f7fffff, 0xffffffff, 0x80000001}
						for i := uint64(0); i < n && pair[1]+i*4+4 <= 65536; i++ {
							binary.LittleEndian.PutUint32(mem[pair[1]+i*4:], values[i%uint64(len(values))])
						}
					}
					init(ref.jm.CurrentBytes())
					copy(got.jm.CurrentBytes(), ref.jm.CurrentBytes())
					copy(fast.jm.CurrentBytes(), ref.jm.CurrentBytes())
					scale, bias := uint64(0xffffffff), uint64(0x80000001)
					if name == "map-f32" {
						scale, bias = uint64(math.Float32bits(1.0000001)), uint64(math.Float32bits(-1))
					}
					args := []uint64{pair[0], pair[1], n, scale, bias}
					a, b := ref.call(args...), got.call(args...)
					if (a != nil) != (b != nil) || a == nil && !bytes.Equal(ref.out, got.out) || !bytes.Equal(ref.jm.CurrentBytes(), got.jm.CurrentBytes()) {
						t.Fatalf("n=%d pair=%v errors=%v/%v outputs=%x/%x", n, pair, a, b, ref.out[:32], got.out[:32])
					}
					endD, endS := pair[0]+n*4, pair[1]+n*4
					expectFast := endD <= 65536 && endS <= 65536 && (pair[0] == pair[1] || endD <= pair[1] || endS <= pair[0])
					fe := fast.call(args...)
					if expectFast && fe != nil {
						t.Fatal("guard failed for eligible input", n, pair, fe)
					}
					if !expectFast && fe == nil {
						t.Fatal("guard incorrectly passed", n, pair)
					}
				}
			}
			if diagnosticsEnabled {
				for _, variant := range []string{"", mode} {
					var stats ModuleStats
					cm, err := CompileModuleWith(m, CompileOptions{AMD64FeaturesSet: true, ExperimentalLoopMode: variant, Stats: &stats})
					if err != nil {
						t.Fatal(err)
					}
					x := stats.Funcs[0]
					t.Logf("mode=%s module=%d function=%d spills=%d reloads=%d frame=%d", variant, len(cm.Code), x.CodeBytes, x.Spills, x.Reloads, x.FrameBytes)
					cm.CodeImage.Close()
				}
			}
		})
	}
}
