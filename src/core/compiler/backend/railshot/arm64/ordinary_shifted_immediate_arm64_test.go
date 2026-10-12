//go:build arm64

package arm64

import (
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestOrdinaryShiftedImmediateSourceLiveAdmission(t *testing.T) {
	requireCompilerDiagnostics(t)
	before := shiftedAddSubImmediateEnabled
	shiftedAddSubImmediateEnabled = true
	t.Cleanup(func() { shiftedAddSubImmediateEnabled = before })
	for _, wide := range []bool{false, true} {
		for _, negative := range []bool{false, true} {
			typ, constant, add, store, load, align := wasm.I32, byte(0x41), byte(0x6a), byte(0x36), byte(0x28), byte(2)
			instruction := uint32(0x11400000) // ADD Wd,Wn,#1,LSL#12
			if wide {
				typ, constant, add, store, load, align = wasm.I64, 0x42, 0x7c, 0x37, 0x29, 3
				instruction = 0x91400000
			}
			lastLEB := byte(0x20) // +4096
			if negative {
				lastLEB = 0x60            // -4096
				instruction |= 0x40000000 // SUB instead of ADD
			}
			// Store x+disp before rereading x. The store eagerly materializes the
			// borrowed add without a destination hint, so this exercises addDisp
			// and would expose an accidental overwrite of the live source pin.
			body := []byte{0, 0x41, 0, 0x20, 0, constant, 0x80, lastLEB, add, store, align, 0,
				0x41, 0, load, align, 0, 0x20, 0, add, 0x0b}
			m := mod1(t, []wasm.ValType{typ}, []wasm.ValType{typ}, body)
			m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
			for _, compact := range []bool{false, true} {
				for _, on := range []bool{false, true} {
					for _, guard := range []bool{false, true} {
						var stats ModuleStats
						opts := CompileOptions{CompactNative: compact, ElideBoundsChecks: guard, Stats: &stats, Optimizations: map[string]bool{"ordinary-shifted-immediate": on}}
						cm, err := CompileModuleWith(m, opts)
						if err != nil {
							t.Fatal(err)
						}
						matches := 0
						for pc := 0; pc+4 <= len(cm.Code); pc += 4 {
							word := binary.LittleEndian.Uint32(cm.Code[pc : pc+4])
							if word&0xffc00000 == instruction && word>>10&0xfff == 1 {
								matches++
								if word&31 == word>>5&31 {
									t.Fatal("shifted displacement overwrites its live source")
								}
							}
						}
						if cm.CodeImage != nil {
							cm.CodeImage.Close()
						}
						wantHits := 0
						if compact || on {
							wantHits = 1
						}
						if stats.Funcs[0].Peephole["shifted-add-sub-immediate"] != wantHits || matches != wantHits {
							t.Fatalf("wide=%v negative=%v compact=%v on=%v guard=%v hits=%v native=%d want=%d", wide, negative, compact, on, guard, stats.Funcs[0].Peephole, matches, wantHits)
						}
						opts.Stats = nil
						for _, input := range []uint64{7, 0xffffffffffffffff, 0xfeedbeef80000001} {
							want := input + input + 4096
							if negative {
								want = input + input - 4096
							}
							if !wide {
								want = uint64(uint32(want))
							}
							got, err := runArm64WrapperWithOptions(t, m, opts, input)
							if err != nil || got != want {
								t.Fatalf("live source wide=%v negative=%v compact=%v on=%v guard=%v input=%x got=%x want=%x err=%v", wide, negative, compact, on, guard, input, got, want, err)
							}
						}
					}
				}
			}
		}
	}
}

func TestOrdinaryShiftedImmediateNative(t *testing.T) {
	leb := func(v int64) []byte {
		var out []byte
		for {
			b := byte(v) & 127
			v >>= 7
			done := v == 0 && b&64 == 0 || v == -1 && b&64 != 0
			if !done {
				b |= 128
			}
			out = append(out, b)
			if done {
				return out
			}
		}
	}
	for _, wide := range []bool{false, true} {
		for _, c := range []int64{4096, -4096, 65536, -65536, 0xfff000, -0xfff000, 4097} {
			for _, kind := range []string{"add", "sub", "eq", "lt"} {
				t.Run(fmt.Sprintf("wide=%v/c=%d/%s", wide, c, kind), func(t *testing.T) {
					typ, constant, op, result := wasm.I32, byte(0x41), byte(0x6a), wasm.I32
					if wide {
						typ, constant, op, result = wasm.I64, 0x42, 0x7c, wasm.I64
					}
					switch kind {
					case "sub":
						op++
					case "eq":
						op, result = 0x46, wasm.I32
						if wide {
							op = 0x51
						}
					case "lt":
						op, result = 0x48, wasm.I32
						if wide {
							op = 0x53
						}
					}
					body := append([]byte{0, 0x20, 0, constant}, leb(c)...)
					body = append(body, op, 0x0b)
					m := mod1(t, []wasm.ValType{typ}, []wasm.ValType{result}, body)
					for _, input := range []uint64{0, uint64(c), 0x80000000, 0x8000000000000000, 0xffffffffffffffff} {
						x := input
						if !wide {
							x = uint64(uint32(x))
						}
						var want uint64
						switch kind {
						case "add":
							want = x + uint64(c)
						case "sub":
							want = x - uint64(c)
						case "eq":
							if wide && x == uint64(c) || !wide && uint32(x) == uint32(c) {
								want = 1
							}
						case "lt":
							if wide && int64(x) < c || !wide && int32(x) < int32(c) {
								want = 1
							}
						}
						if !wide && (kind == "add" || kind == "sub") {
							want = uint64(uint32(want))
						}
						for _, guard := range []bool{false, true} {
							for _, on := range []bool{false, true} {
								got, err := runArm64WrapperWithOptions(t, m, CompileOptions{ElideBoundsChecks: guard, Optimizations: map[string]bool{"ordinary-shifted-immediate": on}}, input)
								if err != nil || got != want {
									t.Fatalf("input=%x guard=%v on=%v got=%x want=%x err=%v", input, guard, on, got, want, err)
								}
							}
						}
					}
				})
			}
		}
	}
}
