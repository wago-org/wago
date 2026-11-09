//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestShortALUCompareBranchAcrossLayouts(t *testing.T) {
	old, oldCover := shiftedALUComparePaddingEnabled, shiftedALUImmediateEnabled
	defer func() {
		shiftedALUComparePaddingEnabled, shiftedALUImmediateEnabled = old, oldCover
	}()
	shiftedALUImmediateEnabled = true
	hits := 0
	for _, wide := range []bool{false, true} {
		typ, c, add, gt, xor := wasm.I32, byte(0x41), byte(0x6a), byte(0x4a), byte(0x73)
		if wide {
			typ, c, add, gt, xor = wasm.I64, 0x42, 0x7c, 0x55, 0x85
		}
		blockType := byte(0x7f)
		if wide {
			blockType = 0x7e
		}
		for prefix := 1; prefix <= 8; prefix++ {
			body := []byte{0}
			for i := 0; i < prefix; i++ {
				body = append(body, 0x41, byte(i), 0x41, 42, 0x3a, 0, 0)
			}
			sub := byte(0x6b)
			if wide {
				sub = 0x7d
			}
			body = append(body, 0x20, 0, c, 1, sub, c)
			body = append(body, wasmtest.SLEB64(-32768)...)
			body = append(body, add, c, 7, gt, 0x04, blockType, c, 1, 0x05, c, 2, 0x0b, 0x20, 0, xor, 0x0b)
			m := mod1(t, []wasm.ValType{typ}, []wasm.ValType{typ}, body)
			m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
			for _, on := range []bool{false, true} {
				shiftedALUComparePaddingEnabled = on
				for _, compact := range []bool{false, true} {
					for _, x := range []uint64{0, 1, 32767, 32768, 32769, 0x80000000, 0xffffffff, 0xfeed00000001, 0x7fffffffffffffff, 0x8000000000000000, 0xffffffffffffffff} {
						positive := int64(x-32769) > 7
						if !wide {
							positive = int32(uint32(x)-32769) > 7
						}
						want := uint64(2)
						if positive {
							want = 1
						}
						want ^= x
						if !wide {
							want = uint64(uint32(want))
						}
						var stats ModuleStats
						opts := CompileOptions{CompactNative: compact, Optimizations: map[string]bool{"ordinary-shifted-immediate": true}}
						if diagnosticsEnabled {
							opts.Stats = &stats
						}
						got, err := runArm64WrapperWithOptions(t, m, opts, x)
						if err != nil || got != want {
							t.Fatalf("wide%v prefix%d on%v compact%v input%x got%x want%x: %v", wide, prefix, on, compact, x, got, want, err)
						}
						if diagnosticsEnabled {
							n := stats.Funcs[0].Peephole["short-alu-compare-padding"]
							if (!on || compact) && n != 0 {
								t.Fatal("padding on excluded path")
							}
							hits += n
						}
					}
				}
			}
		}
	}
	if diagnosticsEnabled && hits == 0 {
		t.Fatal("fixtures did not exercise compare boundary padding")
	}
}
