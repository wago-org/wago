//go:build arm64

package arm64

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
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

// Capture a complete source iteration before its first store. Native vector
// groups must not overwrite values captured by a later group in that iteration.
func TestFloatMapCapturedIterationOverlap(t *testing.T) {
	for _, wide := range []bool{false, true} {
		width, load, store, typ := uint32(4), byte(0x2a), byte(0x38), byte(0x7d)
		if wide {
			width, load, store, typ = 8, 0x2b, 0x39, 0x7c
		}
		for _, count := range []uint32{2, 4, 8} {
			for _, distance := range []int32{-32, -16, -4, 0, 4, 16, 32, 64, 512} {
				t.Run(fmt.Sprintf("wide%t/count%d/distance%d", wide, count, distance), func(t *testing.T) {
					body := []byte{2, 3, 0x7f, byte(count), typ}
					constant := func(v uint32) { body = append(body, 0x41); body = append(body, wasmtest.SLEB32(int32(v))...) }
					constant(4096)
					body = append(body, 0x21, 0)
					constant(uint32(4096 + distance))
					body = append(body, 0x21, 1, 0x03, 0x40)
					address := func(base byte, off uint32) {
						body = append(body, 0x20, base, 0x20, 2, 0x6a)
						constant(off)
						body = append(body, 0x6a)
					}
					for i := uint32(0); i < count; i++ {
						address(0, i*width)
						body = append(body, load, 0, 0, 0x21, byte(3+i))
					}
					for i := uint32(0); i < count; i++ {
						address(1, i*width)
						body = append(body, 0x20, byte(3+i), store, 0, 0)
					}
					body = append(body, 0x20, 2)
					constant(count * width)
					body = append(body, 0x6a, 0x22, 2)
					constant(256)
					body = append(body, 0x47, 0x0d, 0, 0x0b, 0x20, 2, 0x0b)
					m := modMem(t, 1, nil, []wasm.ValType{wasm.I32}, body)
					initial := make([]byte, 65536)
					for i := 0; i < len(initial)/4; i++ {
						binary.LittleEndian.PutUint32(initial[4*i:], math.Float32bits(float32(i)))
					}
					want, wm, we, _ := runFloatMapMemory(t, m, false, initial)
					got, gm, ge, hits := runFloatMapMemory(t, m, true, initial)
					if we != nil || ge != nil || got != want || !bytes.Equal(wm, gm) {
						t.Fatalf("got %x/%v want %x/%v memory equal %t", got, ge, want, we, bytes.Equal(wm, gm))
					}
					if diagnosticsEnabled && hits != 1 {
						t.Fatal("map candidate was not emitted")
					}
				})
			}
		}
	}
}
