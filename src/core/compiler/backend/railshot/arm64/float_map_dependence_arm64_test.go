//go:build arm64

package arm64

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"
)

func TestFloatMapPacketDependenceDistances(t *testing.T) {
	distances := []int32{-33, -17, -16, -15, -8, -1, 0, 1, 3, 4, 7, 8, 12, 15, 16, 17, 20, 24, 31, 32, 33, 47, 48, 63, 64, 65}
	for _, wide := range []bool{false, true} {
		width := uint32(4)
		if wide {
			width = 8
		}
		initial := make([]byte, 65536)
		for i := uint32(0); i < 65536/width; i++ {
			if wide {
				binary.LittleEndian.PutUint64(initial[i*width:], math.Float64bits(float64(int32(i%47)-23)*0.125))
			} else {
				binary.LittleEndian.PutUint32(initial[i*width:], math.Float32bits(float32(int32(i%47)-23)*0.125))
			}
		}
		for _, stores := range []int{1, 2, 3, 4, 5, 6, 8} {
			step := width * uint32(stores)
			packet := floatMapPacketWidth(step)
			for _, length := range []uint32{step, 2 * step, packet + step, 4*packet + step, 8*packet + step} {
				for _, distance := range distances {
					for _, neighbor := range []uint32{0, width, 16, 32} {
						// Separate source base locals force runtime disambiguation. These
						// intervals stay in bounds; positive long distances form real carried
						// memory recurrences and shorter distances must retain scalar order.
						src := uint32(4096)
						dst := uint32(int32(src) + distance)
						m := floatMapModule(t, wide, stores, src, dst, 0, length, neighbor)
						want, wm, we, _ := runFloatMapMemory(t, m, false, initial)
						got, gm, ge, hits := runFloatMapMemory(t, m, true, initial)
						if we != nil || ge != nil || got != want || !bytes.Equal(wm, gm) {
							t.Fatalf("wide%t stores%d length%d distance%d neighbor%d: got%x/%v want%x/%v memory%t", wide, stores, length, distance, neighbor, got, ge, want, we, bytes.Equal(wm, gm))
						}
						if diagnosticsEnabled && hits != 1 {
							t.Fatalf("missing dynamic-distance admission wide%t stores%d", wide, stores)
						}
					}
				}
			}
		}
	}
}
