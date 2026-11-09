//go:build arm64

package arm64

import (
	"bytes"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestShiftedALUAddressScopeKeepsMemoryLowering(t *testing.T) {
	old := shiftedALUImmediateEnabled
	defer func() { shiftedALUImmediateEnabled = old }()
	for _, wide := range []bool{false, true} {
		typ, c, add, typByte := wasm.I32, byte(0x41), byte(0x6a), byte(0x7f)
		if wide {
			typ, c, add, typByte = wasm.I64, 0x42, 0x7c, 0x7e
		}
		for _, direct := range []bool{false, true} {
			body := []byte{1, 1, typByte}
			constant := func(v int64) { body = append(body, c); body = append(body, wasmtest.SLEB64(v)...) }
			address := func() { body = append(body, 0x20, 0); constant(65536); body = append(body, add) }
			if !direct {
				address()
				body = append(body, 0x21, 1)
			}
			for lane := byte(0); lane < 2; lane++ {
				constant(int64(lane) * 8)
				if direct {
					address()
				} else {
					body = append(body, 0x20, 1)
				}
				body = append(body, 0x29, 0, lane*8, 0x37, 0, 0)
			}
			body = append(body, 0x41, 0, 0x0b)
			m := mod1(t, []wasm.ValType{typ}, []wasm.ValType{wasm.I32}, body)
			m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 2, Addr64: wide}}}
			if err := wasm.ValidateModule(m); err != nil {
				t.Fatal(err)
			}
			for _, compact := range []bool{false, true} {
				var previous []byte
				for _, on := range []bool{false, true} {
					shiftedALUImmediateEnabled = on
					opts := CompileOptions{CompactNative: compact}
					cm, err := CompileModuleWith(m, opts)
					if err != nil {
						t.Fatal(err)
					}
					if previous == nil {
						previous = append([]byte(nil), cm.Code...)
					} else if !bytes.Equal(previous, cm.Code) {
						t.Fatalf("address lowering changed: wide%v direct%v compact%v", wide, direct, compact)
					}
					if cm.CodeImage != nil {
						cm.CodeImage.Close()
					}
					for _, base := range []uint64{0, 1, 17, 65520, 65528, 65535, 65536, 0xffff0000, 0xfffffff0, 0xffffffffffff0000, 0xfffffffffffffff0} {
						initial := bytes.Repeat([]byte{0x39}, 131072)
						for i := range initial {
							initial[i] ^= byte(i)
						}
						want := append([]byte(nil), initial...)
						ea := base + 65536
						if !wide {
							ea = uint64(uint32(base) + 65536)
						}
						traps := false
						for lane := uint64(0); lane < 2; lane++ {
							at := ea + lane*8
							if at < ea || at > uint64(len(want))-8 {
								traps = true
								break
							}
							copy(want[lane*8:lane*8+8], want[at:at+8])
						}
						arg := base
						if !wide {
							arg = uint64(uint32(base)) | 0xfeed00000000
						}
						got, err := runLinearSpanMemorySize(t, m, opts, len(initial), initial, arg)
						if (err != nil) != traps || !bytes.Equal(got, want) {
							t.Fatalf("memory wide%v direct%v compact%v on%v base%x traps%v: %v", wide, direct, compact, on, base, traps, err)
						}
					}
				}
			}
		}
	}
}
