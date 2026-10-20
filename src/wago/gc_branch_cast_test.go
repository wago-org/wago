//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package wago

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// The probe takes a runtime eqref and returns prefix + 16 for a taken branch,
// prefix + 17 for fallthrough. The 16 comes from comparing the selected
// reference with the original. The loop keeps the input live across calls.
func branchCastModule(sub byte, target []byte, nullable bool, depth int, collect bool) []byte {
	types := [][]byte{
		{0x50, 0, 0x5f, 1, 0x7f, 0},             // 0: open struct { i32 }
		{0x4f, 1, 0, 0x5f, 2, 0x7f, 0, 0x7f, 0}, // 1: final subtype { i32, i32 }
		{0x5e, 0x7f, 1},                         // 2: array i32
		{0x5f, 1, 0x7e, 0},                      // 3: unrelated struct { i64 }
		[]byte{0x60, 2, 0x7f, 0x7f, 1, 0x6d},
		[]byte{0x60, 2, 0x6d, 0x7f, 1, 0x7f},
		wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}),
		[]byte{0x60, 0, 2, 0x7f, 0x6d},
	}
	// make(selector, payload): null, i31, base, subtype, array, unrelated.
	makeBody := []byte{}
	values := [][]byte{
		{0xd0, 0x6d}, {0x20, 1, 0xfb, 28}, {0x20, 1, 0xfb, 0, 0},
		{0x20, 1, 0x20, 1, 0xfb, 0, 1}, {0x20, 1, 0xfb, 7, 2},
	}
	for i, value := range values {
		makeBody = append(makeBody, 0x20, 0, 0x41, byte(i), 0x46, 0x04, 0x6d)
		makeBody = append(makeBody, value...)
		makeBody = append(makeBody, 0x05)
	}
	makeBody = append(makeBody, 0x20, 1, 0xad, 0xfb, 0, 3)
	for range values {
		makeBody = append(makeBody, 0x0b)
	}
	makeBody = append(makeBody, 0x0b)
	probe := []byte{1, 1, 0x6d} // one eqref local
	for i := 0; i <= depth; i++ {
		probe = append(probe, 0x02, 7)
	}
	flags := byte(1) // nullable source
	if nullable {
		flags |= 2
	}
	probe = append(probe, 0x20, 1, 0x20, 0, 0xfb, sub, flags)
	probe = append(probe, wasmtest.ULEB(uint32(depth))...)
	probe = append(probe, 0x6d) // any source heap
	probe = append(probe, target...)
	probe = append(probe, 0x21, 2, 0x41, 1, 0x6a, 0x20, 2)
	for i := 0; i <= depth; i++ {
		probe = append(probe, 0x0b)
	}
	if collect {
		// Only the selected stack reference is live at this allocation. Read its
		// payload after collection to detect a lost root or stale handle.
		probe = append(probe, 0xfb, 22, 0, 0xfb, 1, 0, 0x1a, 0xfb, 2, 0, 0, 0x6a, 0x0b)
	} else {
		probe = append(probe, 0x20, 0, 0xd3, 0x41, 16, 0x6c, 0x6a, 0x0b)
	}
	probe = append(wasmtest.ULEB(uint32(len(probe))), probe...)
	// run(selector, payload, iterations), locals: reference, counter, sum.
	run := []byte{2, 1, 0x6d, 2, 0x7f,
		0x20, 0, 0x20, 1, 0x10, 0, 0x21, 3,
		0x02, 0x40, 0x03, 0x40,
		0x20, 4, 0x20, 2, 0x4f, 0x0d, 1,
		0x20, 5, 0x20, 3, 0x41, 32, 0x10, 1, 0x6a, 0x21, 5,
		0x20, 4, 0x41, 1, 0x6a, 0x21, 4, 0x0c, 0,
		0x0b, 0x0b, 0x20, 5, 0x0b}
	run = append(wasmtest.ULEB(uint32(len(run))), run...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec([]byte{4}, []byte{5}, []byte{6})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 2))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(makeBody), probe, run)),
	)
}

func TestGCBranchCast(t *testing.T) {
	for _, sub := range []byte{24, 25} {
		for _, target := range []struct {
			name    string
			heap    []byte
			matches []bool
			native  bool
		}{
			{"base", []byte{0}, []bool{false, false, true, true, false, false}, true},
			{"subtype", []byte{1}, []bool{false, false, false, true, false, false}, true},
			{"array", []byte{2}, []bool{false, false, false, false, true, false}, true},
			{"i31", []byte{0x6c}, []bool{false, true, false, false, false, false}, true},
			{"none", []byte{0x71}, []bool{false, false, false, false, false, false}, true},
			{"struct-fallback", []byte{0x6b}, []bool{false, false, true, true, false, true}, false},
		} {
			for _, nullable := range []bool{false, true} {
				for _, depth := range []int{0, 2} {
					t.Run(fmt.Sprintf("op%d/%s/null%t/depth%d", sub, target.name, nullable, depth), func(t *testing.T) {
						c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), branchCastModule(sub, target.heap, nullable, depth, false))
						if err != nil {
							t.Fatal(err)
						}
						defer c.Close()
						in, err := Instantiate(c, InstantiateOptions{})
						if err != nil {
							t.Fatal(err)
						}
						defer in.Close()
						for selector, match := range target.matches {
							if selector == 0 {
								match = nullable
							}
							taken := match == (sub == 24)
							want := uint64(49)
							if taken {
								want = 48
							}
							got, err := in.Invoke("run", uint64(selector), 7, 13)
							if err != nil || len(got) != 1 || got[0] != want*13 {
								t.Fatalf("input%d: %v, %v; want %d", selector, got, err, want*13)
							}

						}
					})
				}
			}
		}
	}
}

func TestGCBranchCastCollection(t *testing.T) {
	for _, sub := range []byte{24, 25} {
		for _, profile := range []GCProfile{GCProfileThroughput, GCProfileTiny} {
			t.Run(fmt.Sprintf("op%d/profile%d", sub, profile), func(t *testing.T) {
				c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), branchCastModule(sub, []byte{0}, false, 2, true))
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{Profile: profile, CollectEveryAlloc: true, TinyCollectEveryAlloc: true, VerifyAfterCollect: true, StressNurseryBytes: 64, TinyHeapBytes: 64, TinyBlockBytes: 16}})
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				want := uint64(49 * 31)
				if sub == 25 {
					want = 50 * 31
				}
				for range 10 {
					got, err := in.Invoke("run", 3, 17, 31)
					if err != nil || len(got) != 1 || got[0] != want {
						t.Fatalf("run: %v, %v; want %d", got, err, want)
					}
				}
				if s := in.gc.Stats(); s.MinorCollections+s.FullCollections == 0 {
					t.Fatal("no collections")
				}
			})
		}
	}
}
