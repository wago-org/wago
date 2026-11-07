//go:build (linux || darwin) && arm64

package arm64

import (
	"bytes"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestWideZeroStoreMemoryAndTrapBoundaries(t *testing.T) {
	old := wideZeroStoreEnabled
	t.Cleanup(func() { wideZeroStoreEnabled = old })
	for _, enabled := range []bool{false, true} {
		wideZeroStoreEnabled = enabled
		for _, compact := range []bool{false, true} {
			for _, guard := range []bool{false, true} {
				for _, offset := range []uint32{0, 7, 128} {
					// Seed nonzero bytes independently before attempting the zero store.
					// A failed full-width check must preserve even its accessible prefix.
					body := []byte{0}
					for _, at := range []int32{65520, 65528} {
						body = append(body, 0x41)
						body = append(body, wasmtest.SLEB32(at)...)
						body = append(body, 0x42, 0x7f, 0x37, 0, 0)
					}
					body = append(body, 0x20, 0, 0x42, 0, 0x37, 0)
					body = append(body, wasmtest.ULEB(offset)...)
					body = append(body, 0x41, 0, 0x0b)
					m := modMem(t, 1, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
					for _, address := range []uint64{0, 1, 65520 - uint64(offset), 65528 - uint64(offset), 65529 - uint64(offset), 65532 - uint64(offset), 65535 - uint64(offset), 65536, 0xffffffff, 0x123400000001} {
						want := make([]byte, 65536)
						for i := 65520; i < len(want); i++ {
							want[i] = 255
						}
						start := uint64(uint32(address)) + uint64(offset)
						trap := start+8 > uint64(len(want))
						if !trap {
							clear(want[start : start+8])
						}
						var stats ModuleStats
						opts := CompileOptions{CompactNative: compact, ElideBoundsChecks: guard}
						if diagnosticsEnabled {
							opts.Stats = &stats
						}
						got, err := runAlignedStoreMemory(t, m, opts, address)
						if (err != nil) != trap || !bytes.Equal(got, want) {
							t.Fatalf("enabled=%v compact=%v guard=%v offset=%d address=%x error=%v wantTrap=%v memoryEqual=%v", enabled, compact, guard, offset, address, err, trap, bytes.Equal(got, want))
						}
						if diagnosticsEnabled {
							hit := stats.Funcs[0].Peephole["wide-zero-store"] != 0
							if hit != (enabled && !guard) {
								t.Fatalf("selection enabled=%v guard=%v hits=%v", enabled, guard, hit)
							}
						}
					}
				}
			}
		}
	}
}
