//go:build linux && amd64

package amd64

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestSelectLocalSinkPreservesOldValues(t *testing.T) {
	old := selectLocalSinkEnabled
	defer func() { selectLocalSinkEnabled = old }()
	for _, targetFirst := range []bool{false, true} {
		for _, tee := range []bool{false, true} {
			for _, pressure := range []int{0, 12} {
				body := []byte{0}
				for range pressure {
					body = append(body, 0x20, 0, 0x41, 1, 0x6a)
				}
				body = append(body, 0x20, 0) // an older read must survive the assignment
				target := []byte{0x20, 0}
				other := []byte{0x20, 1}
				if targetFirst {
					body = append(body, target...)
					body = append(body, other...)
				} else {
					body = append(body, other...)
					body = append(body, target...)
				}
				body = append(body, 0x20, 0, 0x20, 1, 0x4a, 0x1b) // signed x > y
				if tee {
					body = append(body, 0x22, 0)
				} else {
					body = append(body, 0x21, 0, 0x20, 0)
				}
				body = append(body, 0x6a, 0x20, 0, 0x73)
				for range pressure {
					body = append(body, 0x6a)
				}
				body = append(body, 0x0b)
				m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
				for _, on := range []bool{false, true} {
					selectLocalSinkEnabled = on
					var stats ModuleStats
					cm, err := CompileModuleWith(m, CompileOptions{Stats: optionalTestStats(&stats), Optimizations: map[string]bool{"interval-region-pins": false}})
					if err != nil {
						t.Fatal(err)
					}
					if cm.CodeImage != nil {
						defer cm.CodeImage.Close()
					}
					if pressure == 0 && diagnosticsEnabled && (stats.Funcs[0].Peephole["select-local-sink"] != 0) != on {
						t.Fatalf("targetFirst=%v tee=%v on=%v selection=%v", targetFirst, tee, on, stats.Funcs[0].Peephole)
					}
					for _, x := range []uint32{0, 1, 0x7fffffff, 0x80000000, 0xffffffff} {
						for _, y := range []uint32{0, 7, 0x7fffffff, 0x80000000, 0xffffffff} {
							alt := y
							selected := x
							if (int32(x) > int32(y)) != targetFirst {
								selected = alt
							}
							want := (x + selected) ^ selected
							want += uint32(pressure) * (x + 1)
							if got := uint32(runCompiledAmd64u(t, cm, uint64(x), uint64(y))); got != want {
								t.Fatalf("targetFirst=%v tee=%v pressure=%d on=%v x=%x y=%x got=%x want=%x", targetFirst, tee, pressure, on, x, y, got, want)
							}
						}
					}
				}
			}
		}
	}
}

func TestSelectLocalSinkPreservesTrapAndMemory(t *testing.T) {
	old := selectLocalSinkEnabled
	defer func() { selectLocalSinkEnabled = old }()
	for _, div := range []byte{0x6d, 0x6e} {
		for _, targetFirst := range []bool{false, true} {
			body := []byte{0, 0x41, 16, 0x41, 7, 0x36, 2, 0, 0x20, 1, 0x20, 2, div, 0x1a}
			target := []byte{0x20, 0}
			other := []byte{0x20, 1}
			if targetFirst {
				body = append(body, target...)
				body = append(body, other...)
			} else {
				body = append(body, other...)
				body = append(body, target...)
			}
			body = append(body, 0x20, 0, 0x20, 1, 0x4a, 0x1b, 0x22, 0, 0x1a,
				0x41, 20, 0x20, 0, 0x36, 2, 0, 0x20, 0, 0x0b)
			m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
			for _, args := range [][3]uint64{{100, 10, 0}, {100, 10, 2}, {0x80000000, 0x80000000, 0xffffffff}} {
				var expected []byte
				var result uint64
				var trap string
				for _, on := range []bool{false, true} {
					selectLocalSinkEnabled = on
					var stats ModuleStats
					got, mem, err := runMemAmd64WithOptions(t, m, CompileOptions{Stats: optionalTestStats(&stats), Optimizations: map[string]bool{"interval-region-pins": false}}, nil, args[:]...)
					if on && diagnosticsEnabled && stats.Funcs[0].Peephole["select-local-sink"] == 0 {
						t.Fatalf("div=%x targetFirst=%v selection=%v", div, targetFirst, stats.Funcs[0].Peephole)
					}
					if !on {
						expected, result, trap = mem, got, fmt.Sprint(err)
						continue
					}
					if got != result || !bytes.Equal(mem, expected) || fmt.Sprint(err) != trap {
						t.Fatalf("div=%x targetFirst=%v args=%x got=%x want=%x memory=%v trap=%v want=%s", div, targetFirst, args, got, result, bytes.Equal(mem, expected), err, trap)
					}
				}
			}
		}
	}
}

func TestSelectLocalSinkLeavesUnrelatedComparison(t *testing.T) {
	old := selectLocalSinkEnabled
	defer func() { selectLocalSinkEnabled = old }()
	selectLocalSinkEnabled = true
	// The chosen alternatives are x and y, but the predicate compares y and z.
	body := []byte{0, 0x20, 0, 0x20, 1, 0x20, 1, 0x20, 2, 0x4a, 0x1b, 0x22, 0, 0x0b}
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
	var stats ModuleStats
	cm, err := CompileModuleWith(m, CompileOptions{Stats: optionalTestStats(&stats), Optimizations: map[string]bool{"interval-region-pins": false}})
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	if diagnosticsEnabled && stats.Funcs[0].Peephole["select-local-sink"] != 0 {
		t.Fatal("coalesced an unrelated comparison")
	}
	if got := runCompiledAmd64u(t, cm, 7, 2, 3); got != 2 {
		t.Fatalf("got %d, want 2", got)
	}
}
