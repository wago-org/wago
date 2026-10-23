//go:build (linux || darwin || windows) && arm64

package arm64

import (
	"encoding/binary"
	"fmt"
	"reflect"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestFloatSelectDeferredLoadOrder(t *testing.T) {
	for _, alternative := range []struct {
		name  string
		typ   wasm.ValType
		load  []byte
		align byte
	}{{"f32", wasm.F32, []byte{0x2a}, 2}, {"f64", wasm.F64, []byte{0x2b}, 3}, {"v128", wasm.V128, []byte{0xfd, 0}, 4}} {
		for _, wide := range []bool{false, true} {
			load, align, cmp := byte(0x2a), byte(2), byte(0x5b)
			if wide {
				load, align, cmp = 0x2b, 3, 0x61
			}
			for _, typed := range []bool{false, true} {
				for _, invert := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/wide=%v/typed=%v/invert=%v", alternative.name, wide, typed, invert), func(t *testing.T) {
						// Distinct offsets identify alternatives followed by comparands. Each
						// guard-backed load or explicit check can trap; preserve Wasm order.
						body := []byte{0}
						for i := 0; i < 4; i++ {
							body = append(body, 0x20, byte(i))
							if i < 2 {
								body = append(body, alternative.load...)
								body = append(body, alternative.align)
							} else {
								body = append(body, load, align)
							}
							body = append(body, wasmtest.ULEB(uint32((i+1)*64))...)
						}
						body = append(body, cmp)
						if invert {
							body = append(body, 0x45)
						}
						if typed {
							body = append(body, 0x1c, 1, wasm.MustEncodeValType(alternative.typ))
						} else {
							body = append(body, 0x1b)
						}
						body = append(body, 0x0b)
						m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{alternative.typ}, body)
						m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
						for _, flags := range []bool{true, false} {
							for _, guard := range []bool{true, false} {
								cm, err := CompileModuleWith(m, CompileOptions{ElideBoundsChecks: guard, Optimizations: map[string]bool{"st-flags": flags}})
								if err != nil {
									t.Fatal(err)
								}
								want := []uint32{64, 128, 192, 256}
								if !guard {
									// Bounds-check LEAs use offset + access width, rather than the
									// displacement used by the eventual nontrapping native load.
									altSize, cmpSize := uint32(4), uint32(4)
									if alternative.typ == wasm.F64 {
										altSize = 8
									}
									if alternative.typ == wasm.V128 {
										altSize = 16
									}
									if wide {
										cmpSize = 8
									}
									want[0] += altSize
									want[1] += altSize
									want[2] += cmpSize
									want[3] += cmpSize
								}
								var offsets []uint32
								for i := 0; i+4 <= len(cm.Code); i += 4 {
									word := binary.LittleEndian.Uint32(cm.Code[i:])
									// Guard mode identifies displacement ADDs before loads; explicit
									// mode identifies end-address ADDs before bounds checks. Exclude
									// SP-based wrapper adjustments from the latter.
									if guard && word&0xffc003ff == 0x91000210 || !guard && word&0xffc00000 == 0x91000000 && (word>>5)&31 != 31 {
										offset := (word >> 10) & 0xfff
										if offset == want[0] || offset == want[1] || offset == want[2] || offset == want[3] {
											offsets = append(offsets, offset)
										}
									}
								}
								if cm.CodeImage != nil {
									if err := cm.CodeImage.Close(); err != nil {
										t.Fatal(err)
									}
								}
								if !reflect.DeepEqual(offsets, want) {
									t.Fatalf("st-flags=%v guard=%v access/check order=%v; want %v", flags, guard, offsets, want)
								}
							}
						}
					})
				}
			}
		}
	}
}
