//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	rt "github.com/wago-org/wago/src/core/runtime"
)

func TestScalarSelectBranchBodyRequiresOwnedCompareFlags(t *testing.T) {
	for _, body := range [][]byte{
		{0x0f, 0x44, 0xc6},
		{0x85, 0xff, 0x83, 0xc0, 1, 0x0f, 0x44, 0xc6},
		{0x85, 0xff, 0x0f, 0x44, 0x06},
		{0x85, 0xff, 0x0f, 0x44, 0xe0},
		{0x85, 0xff, 0x0f, 0x44, 0xe8},
		{0x85, 0xff, 0xe8, 0, 0, 0, 0},
		{0x85, 0xff, 0x66, 0x0f, 0x44, 0xc6},
	} {
		var out [64]byte
		if _, ok := scalarSelectBranchBody(body, out[:]); ok {
			t.Fatalf("accepted unsafe body %x", body)
		}
	}
	for cc := byte(0); cc < 16; cc++ {
		for _, rex := range []byte{0, 0x45, 0x48, 0x4d} {
			body := []byte{0x85, 0xff}
			if rex != 0 {
				body = append(body, rex)
			}
			body = append(body, 0x0f, 0x40|cc, 0xc6)
			var out [64]byte
			n, ok := scalarSelectBranchBody(body, out[:])
			if !ok {
				t.Fatalf("rejected %x", body)
			}
			want := []byte{0x85, 0xff}
			if rex&8 == 0 {
				if rex&4 != 0 {
					want = append(want, 0x45) // extended r32 destination
				}
				want = append(want, 0x89, 0xc0) // preserve CMOV32's unconditional zero extension
			}
			want = append(want, 0x70|(cc^1), 2)
			if rex != 0 {
				want[len(want)-1] = 3
				want = append(want, rex)
			}
			want = append(want, 0x8b, 0xc6)
			if !bytes.Equal(out[:n], want) {
				t.Fatalf("cc=%x rex=%x got=%x want=%x", cc, rex, out[:n], want)
			}
			if scalarLoopBody(body) {
				t.Fatal("selection admission leaked into ordinary scalar decoder")
			}
		}
	}
}

func TestScalarSelectBranchLoopKeepsEveryExit(t *testing.T) {
	saved := pureSelectEnabled
	pureSelectEnabled = true
	defer func() { pureSelectEnabled = saved }()
	body := []byte{1, 1, 0x7f, 0x03, 0x40,
		0x20, 2, 0x20, 0, 0x41, 1, 0x71, 0x04, 0x7f,
		0x20, 0, 0x41, 3, 0x6c, 0x05, 0x20, 0, 0x41, 7, 0x6a, 0x0b, 0x6a, 0x21, 2,
		0x20, 0, 0x41, 1, 0x6a, 0x22, 0, 0x20, 1, 0x49, 0x0d, 0, 0x0b, 0x20, 2, 0x0b}
	m := modFuncs(t, funcDef{[]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body})
	for _, enabled := range []bool{false, true} {
		for _, compact := range []bool{false, true} {
			for _, interruptible := range []bool{false, true} {
				for _, workers := range []int{1, 4} {
					opts := CompileOptions{Workers: workers, CompactNative: compact, Interruptible: interruptible, Optimizations: map[string]bool{"scalar-loop-unroll": enabled, "canonical-i32": false}}
					var stats ModuleStats
					if diagnosticsEnabled {
						opts.Stats = &stats
					}
					cm, err := CompileModuleWith(m, opts)
					if err != nil {
						t.Fatal(err)
					}
					if cm.CodeImage != nil {
						cm.CodeImage.Close()
					}
					wantMarker := enabled && (nativeCompactionDisabled || !compact && !nativeCompactionEnabled) && !interruptible && !profileEnabled
					if diagnosticsEnabled && (stats.Funcs[0].Peephole["scalar-select-branch-unroll4"] != 0) != wantMarker {
						t.Fatalf("enabled=%v compact=%v interruptible=%v stats=%v", enabled, compact, interruptible, stats.Funcs[0].Peephole)
					}
					if diagnosticsEnabled && (stats.Funcs[0].Peephole["scalar-lazy-select-unroll4"] != 0) != wantMarker {
						t.Fatalf("lazy admission enabled=%v compact=%v interruptible=%v stats=%v", enabled, compact, interruptible, stats.Funcs[0].Peephole)
					}
					opts.Stats = nil
					for _, input := range [][2]uint32{{0, 0}, {7, 3}, {0, 1}, {0, 2}, {0, 3}, {0, 4}, {0, 5}, {0, 8}, {0, 9}, {3, 7}, {0xfffffffe, 2}, {0xffffffff, 1}} {
						i, sum := input[0], uint32(0)
						for {
							v := i + 7
							if i&1 != 0 {
								v = i * 3
							}
							sum += v
							i++
							if i >= input[1] {
								break
							}
						}
						got, err := runPureIfWithOptions(t, m, opts, uint64(input[0])|0xdeadbeef00000000, uint64(input[1])|0xcafebabe00000000)
						if err != nil || uint32(got) != sum {
							t.Fatalf("enabled=%v compact=%v interruptible=%v workers=%d input=%x got=%x want=%x err=%v", enabled, compact, interruptible, workers, input, got, sum, err)
						}
					}
				}
			}
		}
	}
}

// The original CPU instruction is an independent oracle for every condition,
// including upper-half clearing when a 32-bit CMOV is false.
func TestScalarSelectBranchMatchesNativeCmovWithDirtyInputs(t *testing.T) {
	engine, err := rt.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	memory, err := rt.NewJobMemory(65536)
	if err != nil {
		t.Fatal(err)
	}
	defer memory.Close()
	arena, err := rt.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	defer arena.Close()
	args, result, trap := arena.Alloc(256), arena.Alloc(256), arena.Alloc(rt.TrapBufferBytes)
	setup := []byte{0x48, 0x8b, 0x07, 0x4c, 0x8b, 0x4f, 0x08, 0x4c, 0x8b, 0x47, 0x10, 0x4c, 0x8b, 0x57, 0x18}
	tail := []byte{0x48, 0x89, 0x01, 0xc3}
	for cc := byte(0); cc < 16; cc++ {
		for _, wide := range []bool{false, true} {
			compare, selectRex := byte(0x45), byte(0x41)
			if wide {
				compare, selectRex = 0x4d, 0x49
			}
			body := []byte{compare, 0x39, 0xd0, selectRex, 0x0f, 0x40 | cc, 0xc1}
			var transformed [128]byte
			n, ok := scalarSelectBranchBody(body, transformed[:])
			if !ok {
				t.Fatalf("rejected %x", body)
			}
			original := append(append(append([]byte{}, setup...), body...), tail...)
			rewritten := append(append(append([]byte{}, setup...), transformed[:n]...), tail...)
			oldMap, oldEntry, err := rt.MapCode(original)
			if err != nil {
				t.Fatal(err)
			}
			newMap, newEntry, err := rt.MapCode(rewritten)
			if err != nil {
				rt.Unmap(oldMap)
				t.Fatal(err)
			}
			for _, flags := range [][2]uint64{{0, 0}, {0, 1}, {1, 0}, {0xffffffff, 1}, {0x80000000, 0x7fffffff}, {0x8000000000000000, 0x7fffffffffffffff}, {^uint64(0), 0}} {
				inputs := [4]uint64{0xdeadbeef80000001, 0xcafebabefedcba98, flags[0], flags[1]}
				for i, v := range inputs {
					binary.LittleEndian.PutUint64(args[i*8:], v)
				}
				if err := engine.Call(oldEntry, args, memory.LinearMemory(), trap, result); err != nil {
					t.Fatal(err)
				}
				want := binary.LittleEndian.Uint64(result)
				if err := engine.Call(newEntry, args, memory.LinearMemory(), trap, result); err != nil {
					t.Fatal(err)
				}
				got := binary.LittleEndian.Uint64(result)
				if got != want || !wide && got>>32 != 0 {
					t.Fatalf("cc=%x wide=%v flags=%x got=%x original=%x", cc, wide, flags, got, want)
				}
			}
			rt.Unmap(newMap)
			rt.Unmap(oldMap)
		}
	}
}

func TestScalarSelectBranchImmediateTestsAreBounded(t *testing.T) {
	for _, prefix := range [][]byte{{0x41, 0xf6, 0xc1, 1}, {0x40, 0xf6, 0xc6, 0x80}, {0xf7, 0xc1, 1, 0, 0, 0}, {0x49, 0xf7, 0xc1, 0xff, 0xff, 0xff, 0xff}} {
		body := append(append([]byte{}, prefix...), 0x0f, 0x45, 0xc6)
		var out [128]byte
		if _, ok := scalarSelectBranchBody(body, out[:]); !ok {
			t.Fatalf("rejected register test %x", body)
		}
		if scalarLoopBody(body) {
			t.Fatal("test admission leaked into ordinary scalar decoder")
		}
	}
	for _, body := range [][]byte{{0xf6, 0xc6, 1, 0x0f, 0x45, 0xc0}, {0xf6, 0x01, 1, 0x0f, 0x45, 0xc0}, {0xf6, 0xd1, 1, 0x0f, 0x45, 0xc0}, {0x41, 0xf6, 0xc1}, {0xf7, 0xc1, 1}} {
		var out [128]byte
		if _, ok := scalarSelectBranchBody(body, out[:]); ok {
			t.Fatalf("accepted unsupported/truncated test %x", body)
		}
	}
}
