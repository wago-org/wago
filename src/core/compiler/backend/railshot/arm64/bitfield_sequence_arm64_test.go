//go:build (linux || darwin) && arm64

package arm64

import (
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Keep these fixtures on the deferred-tree path. The cached variant uses the
// same mask in a loop multiplication, so loop-int-const may preload it even
// when the following bitfield pattern no longer needs a mask register.
func bitfieldSequenceFixture(t testing.TB, wide, cached bool, family string) (*wasm.Module, []byte) {
	typ, c, mul, sub, shr, shl, and, sshr := wasm.I32, byte(0x41), byte(0x6c), byte(0x6b), byte(0x76), byte(0x74), byte(0x71), byte(0x75)
	if wide {
		typ, c, mul, sub, shr, shl, and, sshr = wasm.I64, 0x42, 0x7e, 0x7d, 0x88, 0x86, 0x83, 0x87
	}
	mask := int64(31)
	if family == "mask-before-shift" || family == "fallback-hole" || family == "fallback-zero" {
		mask = 0xf05
	}
	if family == "fallback-hole" {
		mask = 0x505
	}
	body := []byte{0, 0x3f, 0, 0x1a}
	if cached {
		body = append(body, 0x03, 0x40, 0x20, 1, c)
		body = append(body, wasmtest.SLEB64(mask)...)
		body = append(body, mul, 0x21, 1)
	}
	if family == "register-mask" {
		body = append(body, c, 31, 0x21, 1)
	}
	body = append(body, 0x20, 0)
	switch family {
	case "extract", "extract-local-sink":
		if family == "extract-local-sink" {
			body = append(body, c, 1, sub)
		}
		body = append(body, c, 11, shr, c, 31, and)
	case "register-mask":
		body = append(body, c, 11, shr, 0x20, 1, and)
	case "insert":
		body = append(body, c, 31, and, c, 8, shl)
	case "signed":
		body = append(body, c, 20, shl, c, 27, sshr)
	case "unsigned":
		body = append(body, c, 20, shl, c, 27, shr)
	case "mask-before-shift", "fallback-hole", "fallback-zero":
		body = append(body, c)
		body = append(body, wasmtest.SLEB64(mask)...)
		shift := byte(8)
		if family == "fallback-zero" {
			shift = 0
		}
		body = append(body, and, c, shift, shr)
	default:
		t.Fatalf("unknown family %q", family)
	}
	body = append(body, 0x21, 0)
	if cached {
		body = append(body, 0x0b)
	}
	body = append(body, 0x20, 0, 0x0b)
	m := mod1(t, []wasm.ValType{typ, typ}, []wasm.ValType{typ}, body)
	m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
	return m, body
}

func TestBitfieldSequenceShapeAndValues(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, cached := range []bool{false, true} {
			for _, family := range []string{"extract", "extract-local-sink", "insert", "signed", "unsigned", "mask-before-shift", "fallback-hole", "fallback-zero", "register-mask"} {
				t.Run(fmt.Sprintf("%s/wide=%v/cached=%v", family, wide, cached), func(t *testing.T) {
					m, _ := bitfieldSequenceFixture(t, wide, cached, family)
					var stats ModuleStats
					opts := CompileOptions{}
					if diagnosticsEnabled {
						opts.Stats = &stats
					}
					cm, err := CompileModuleWith(m, opts)
					if err != nil {
						t.Fatal(err)
					}
					if cm.CodeImage != nil {
						defer cm.CodeImage.Close()
					}
					immr, imms, signed := uint32(11), uint32(15), false
					switch family {
					case "insert":
						if wide {
							immr = 56
						} else {
							immr = 24
						}
						imms = 4
					case "signed", "unsigned":
						if wide {
							imms = 43
						} else {
							imms = 11
						}
						immr = 7
						signed = family == "signed"
					case "mask-before-shift", "fallback-hole", "fallback-zero":
						immr = 8
						imms = 11
					}
					opcode := uint32(0x53000000)
					if signed {
						opcode = 0x13000000
					}
					found := 0
					for at := 0; at+4 <= len(cm.Code); at += 4 {
						word := binary.LittleEndian.Uint32(cm.Code[at:])
						if word&0x7f800000 == opcode && word>>16&63 == immr && word>>10&63 == imms {
							found++
						}
					}
					wantPairs := 1
					if family == "fallback-hole" || family == "fallback-zero" || family == "register-mask" {
						wantPairs = 0
					}
					if found != wantPairs {
						t.Fatalf("bounded bitfield count=%d, want %d", found, wantPairs)
					}
					if family == "register-mask" {
						folded := 0
						for at := 0; at+4 <= len(cm.Code); at += 4 {
							word := binary.LittleEndian.Uint32(cm.Code[at:])
							if word&0x7f200000 == 0x0a000000 && word>>22&3 == 1 && word>>10&63 == 11 {
								folded++
							}
						}
						if folded != 1 {
							t.Fatalf("register-mask shifted AND count=%d, want 1", folded)
						}
					}
					if diagnosticsEnabled {
						s := stats.Funcs[0]
						if s.SharedScalar || s.Peephole["bitfield-pair"] != wantPairs {
							t.Fatalf("path not observed: %v", s)
						}
						if cached && s.Peephole["loop-int-const"] == 0 {
							t.Fatalf("mask cache not observed: %v", s.Peephole)
						}
					}
					for _, x := range []uint64{0, 1, 31, 65536, 0x80000000, 0x8000000000000000, ^uint64(0)} {
						if !wide {
							x = uint64(uint32(x))
						}
						v := x
						var want uint64
						switch family {
						case "extract-local-sink":
							v--
							if !wide {
								v = uint64(uint32(v))
							}
							fallthrough
						case "extract", "register-mask":
							want = v >> 11 & 31
						case "insert":
							want = (v & 31) << 8
						case "mask-before-shift":
							want = (v & 0xf05) >> 8
						case "fallback-hole":
							want = (v & 0x505) >> 8
						case "fallback-zero":
							want = v & 0xf05
						case "signed", "unsigned":
							v <<= 20
							if !wide {
								v = uint64(uint32(v))
							}
							if family == "signed" {
								if wide {
									want = uint64(int64(v) >> 27)
								} else {
									want = uint64(uint32(int32(v) >> 27))
								}
							} else {
								want = v >> 27
							}
						}
						got, err := runArm64WrapperWithOptions(t, m, opts, x, 7)
						if err != nil || got != want {
							t.Fatalf("x=%x got=%x want=%x err=%v", x, got, want, err)
						}
					}
				})
			}
		}
	}
}
