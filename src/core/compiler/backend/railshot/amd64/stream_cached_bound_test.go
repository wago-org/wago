//go:build (linux || darwin || windows) && amd64 && !tinygo && !wago_profile

package amd64

import (
	"bytes"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
	"testing"
)

func TestStreamCachedBoundForms(t *testing.T) {
	old := streamingCachedBoundEnabled
	t.Cleanup(func() { streamingCachedBoundEnabled = old })
	for _, enabled := range []bool{false, true} {
		for _, shared := range []bool{false, true} {
			for _, cache := range []Reg{regNone, R15} {
				for _, guard := range []bool{false, true} {
					streamingCachedBoundEnabled = enabled
					f := fn{a: &encoder.Asm{}, memSizeReg: cache, threadedMemory0: shared, guardMode: guard}
					f.streamCompareMemoryEnd(R10)
					var want encoder.Asm
					if enabled && !shared && cache != regNone {
						want.Cmp64(R10, cache)
					} else {
						want.AluRM(cmpRMcode, R10, RBX, -bdCurBytes, true)
					}
					if !bytes.Equal(f.a.B, want.B) {
						t.Fatalf("enabled=%v shared=%v cache=%v guard=%v got=%x want=%x", enabled, shared, cache, guard, f.a.B, want.B)
					}
				}
			}
		}
	}
}

func TestStreamCachedBoundRuntime(t *testing.T) {
	old := streamingCachedBoundEnabled
	t.Cleanup(func() { streamingCachedBoundEnabled = old })
	streamingCachedBoundEnabled = true
	// Reuse real-runtime fixtures for boundary equality, huge memarg offsets,
	// dirty carriers, bulk pre-write checks and eager source-PC traps.
	t.Run("loads", TestStreamMemoryExecuteLoads)
	t.Run("stores-offsets", TestStreamMemoryExecuteOffsetsAndStores)
	t.Run("bulk", TestStreamMemoryExecuteBulk)
	t.Run("dropped-trap", TestStreamMemoryDroppedLoadTrapPC)
	t.Run("legacy-growth", TestStreamCallLegacyMemoryGrow)
}

func TestStreamCachedBoundRegionalLeaseCall(t *testing.T) {
	oldCache, oldStream, oldJoint, oldInline := streamingCachedBoundEnabled, streamingOperandsEnabled, streamingCallGlobalsEnabled, inlineEnabled
	t.Cleanup(func() {
		streamingCachedBoundEnabled, streamingOperandsEnabled, streamingCallGlobalsEnabled, inlineEnabled = oldCache, oldStream, oldJoint, oldInline
	})
	streamingOperandsEnabled, streamingCallGlobalsEnabled, inlineEnabled = true, true, false
	// Twenty active integer locals qualify the existing interval-region policy.
	// memory.size is intentionally unsupported by stream admission, forcing the
	// callee to the legacy path without adding calls or control-flow barriers.
	callee := []byte{1, 20, 0x7f, 0x3f, 0, 0x1a}
	for x := byte(0); x < 20; x++ {
		callee = append(callee, 0x41, x+1, 0x21, x)
	}
	callee = append(callee, 0x20, 0)
	for x := byte(1); x < 20; x++ {
		callee = append(callee, 0x20, x, 0x6a)
	}
	callee = append(callee, 0xb)
	for _, cached := range []bool{false, true} {
		for _, address := range []uint64{65535, 65536} {
			streamingCachedBoundEnabled = cached
			m := modFuncs(t, funcDef{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: []byte{0, 0x10, 1, 0x1a, 0x20, 0, 0x2d, 0, 0, 0xb}}, funcDef{results: []wasm.ValType{wasm.I32}, body: callee})
			m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
			var stats ModuleStats
			opts := CompileOptions{Optimizations: map[string]bool{"memsize-regional-lease": true, "interval-region-pins": true}}
			if diagnosticsEnabled {
				opts.Stats = &stats
			}
			got, _, err := runMemAmd64WithOptions(t, m, opts, func(mem []byte) { mem[65535] = 0x5a }, address)
			if (err != nil) != (address == 65536) || err == nil && got != 0x5a {
				t.Fatalf("cached=%v addr=%d got=%x err=%v", cached, address, got, err)
			}
			if diagnosticsEnabled && !regallocCheckEnabled {
				if stats.Funcs[0].Peephole["streaming-direct-call"] != 1 || stats.Funcs[1].Peephole["memsize-regional-lease"] != 1 || stats.Funcs[1].Peephole["streaming-function"] != 0 {
					t.Fatalf("fixture missed call/legacy lease: caller=%v callee=%v", stats.Funcs[0].Peephole, stats.Funcs[1].Peephole)
				}
				want := uint64(0)
				if cached {
					want = 1
				}
				if uint64(stats.Funcs[0].Peephole["stream-cached-bound"]) != want {
					t.Fatal("cached-bound native path not selected as requested")
				}
			}
		}
	}
}
