//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package wago

import (
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// The probe takes a runtime eqref and returns prefix + 16 for a taken branch,
// prefix + 17 for fallthrough. The 16 comes from comparing the selected
// reference with the original. The loop keeps the input live across calls.
func branchCastModule(sub byte, target []byte, nullable bool, depth int, collect, nonNullSource bool) []byte {
	types := [][]byte{
		{0x50, 0, 0x5f, 1, 0x7f, 0},             // 0: open struct { i32 }
		{0x4f, 1, 0, 0x5f, 2, 0x7f, 0, 0x7f, 0}, // 1: final subtype { i32, i32 }
		{0x5e, 0x7f, 1},                         // 2: array i32
		{0x5f, 1, 0x7e, 0},                      // 3: unrelated struct { i64 }
		[]byte{0x60, 2, 0x7f, 0x7f, 1, 0x6d},
		[]byte{0x60, 2, 0x6d, 0x7f, 1, 0x7f},
		wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}),
		[]byte{0x60, 0, 2, 0x7f, 0x6d},
		{0x4f, 1, 0, 0x5f, 2, 0x7f, 0, 0x7f, 0}, // 8: canonical twin of final subtype 1
	}
	// make: null, i31, base, subtype, array, unrelated, canonical subtype twin.
	makeBody := []byte{}
	values := [][]byte{
		{0xd0, 0x6d}, {0x20, 1, 0xfb, 28}, {0x20, 1, 0xfb, 0, 0},
		{0x20, 1, 0x20, 1, 0xfb, 0, 1}, {0x20, 1, 0xfb, 7, 2},
		{0x20, 1, 0xad, 0xfb, 0, 3},
	}
	for i, value := range values {
		makeBody = append(makeBody, 0x20, 0, 0x41, byte(i), 0x46, 0x04, 0x6d)
		makeBody = append(makeBody, value...)
		makeBody = append(makeBody, 0x05)
	}
	makeBody = append(makeBody, 0x20, 1, 0x20, 1, 0xfb, 0, 8)
	for range values {
		makeBody = append(makeBody, 0x0b)
	}
	makeBody = append(makeBody, 0x0b)
	probe := []byte{1, 1, 0x6d} // one eqref local
	for i := 0; i <= depth; i++ {
		probe = append(probe, 0x02, 7)
	}
	flags := byte(1) // nullable source
	if nonNullSource {
		flags = 0
	}
	if nullable {
		flags |= 2
	}
	probe = append(probe, 0x20, 1, 0x20, 0)
	if nonNullSource {
		probe = append(probe, 0xd4)
	}
	probe = append(probe, 0xfb, sub, flags)
	probe = append(probe, wasmtest.ULEB(uint32(depth))...)
	probe = append(probe, 0x6d) // any source heap
	probe = append(probe, target...)
	probe = append(probe, 0x21, 2, 0x41, 1, 0x6a, 0x20, 2)
	for i := 0; i <= depth; i++ {
		probe = append(probe, 0x0b)
	}
	if collect {
		// The selected stack reference must remain usable after this allocation.
		// The caller also retains the loop input across probe calls.
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
	requireCompleteCore3Backend(t)
	for _, sub := range []byte{24, 25} {
		for _, target := range []struct {
			name    string
			heap    []byte
			matches []bool
		}{
			{"base", []byte{0}, []bool{false, false, true, true, false, false, true}},
			{"subtype", []byte{1}, []bool{false, false, false, true, false, false, true}},
			{"array", []byte{2}, []bool{false, false, false, false, true, false, false}},
			{"i31", []byte{0x6c}, []bool{false, true, false, false, false, false, false}},
			{"none", []byte{0x71}, []bool{false, false, false, false, false, false, false}},
			{"struct-fallback", []byte{0x6b}, []bool{false, false, true, true, false, true, true}},
		} {
			for _, nullable := range []bool{false, true} {
				for _, depth := range []int{0, 2} {
					t.Run(fmt.Sprintf("op%d/%s/null%t/depth%d", sub, target.name, nullable, depth), func(t *testing.T) {
						c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), branchCastModule(sub, target.heap, nullable, depth, false, false))
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
	requireCompleteCore3Backend(t)
	for _, sub := range []byte{24, 25} {
		for _, target := range []byte{0, 2, 0x6c, 0x71} {
			for _, profile := range []GCProfile{GCProfileThroughput, GCProfileTiny} {
				t.Run(fmt.Sprintf("op%d/target%x/profile%d", sub, target, profile), func(t *testing.T) {
					c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), branchCastModule(sub, []byte{target}, false, 2, true, false))
					if err != nil {
						t.Fatal(err)
					}
					defer c.Close()
					in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{Profile: profile, CollectEveryAlloc: true, TinyCollectEveryAlloc: true, VerifyAfterCollect: true, StressNurseryBytes: 64, TinyHeapBytes: 64, TinyBlockBytes: 16}})
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					want := uint64(50 * 31)
					if (target == 0) == (sub == 24) {
						want = 49 * 31
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
}

// Deferred shifts and loads must not overwrite the match register at the join.
func TestGCBranchCastDeferredPrefix(t *testing.T) {
	requireCompleteCore3Backend(t)
	for n := 0; n < 12; n++ {
		for _, sub := range []byte{24, 25} {
			for _, target := range []byte{0x71, 0x6c} {
				for _, nullable := range []bool{false, true} {
					t.Run(fmt.Sprintf("n%d-sub%d-t%d-null%v", n, sub, target, nullable), func(t *testing.T) {
						body := []byte{0x02, 0x6d, 0x20, 0, 0x20, 1, 0x74}
						for i := 0; i < n; i++ {
							body = append(body, 0x20, 0, 0x28, 0, 0)
						}
						flags := byte(1)
						if nullable {
							flags |= 2
						}
						body = append(body, 0xd0, 0x6d, 0xfb, sub, flags, 0, 0x6d, target, 0x1a)
						for i := 0; i <= n; i++ {
							body = append(body, 0x1a)
						}
						body = append(body, 0x41, 1, 0x21, 2, 0xd0, 0x6d, 0x0b, 0x1a, 0x20, 2, 0x0b)
						data := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 0}, wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))), wasmtest.Section(3, wasmtest.Vec([]byte{1})), wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(branchCastPressureCode(body))))
						c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
						if err != nil {
							t.Fatal(err)
						}
						defer c.Close()
						in, err := Instantiate(c, InstantiateOptions{})
						if err != nil {
							t.Fatal(err)
						}
						defer in.Close()
						got, err := in.Invoke("run", 1, 1)
						want := uint64(1)
						if nullable == (sub == 24) {
							want = 0
						}
						if err != nil || len(got) != 1 || got[0] != want {
							t.Fatalf("got%v,%v want%d", got, err, want)
						}
						if n != 0 {
							_, err := in.Invoke("run", 65536, 1)
							var trap *TrapError
							if !errors.As(err, &trap) || trap.Code != TrapLinMemOutOfBounds {
								t.Fatalf("deferred load trap: %v", err)
							}
						}
					})
				}
			}
		}
	}
}

func branchCastPressureCode(body []byte) []byte {
	b := append([]byte{1, 1, 0x7f}, body...)
	return append(wasmtest.ULEB(uint32(len(b))), b...)
}

func TestGCBranchCastNonNullableSource(t *testing.T) {
	requireCompleteCore3Backend(t)
	for _, sub := range []byte{24, 25} {
		for _, target := range []byte{0, 1, 2, 0x6c, 0x71} {
			for _, nullable := range []bool{false, true} {
				t.Run(fmt.Sprintf("op%d/target%x/null%t", sub, target, nullable), func(t *testing.T) {
					c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), branchCastModule(sub, []byte{target}, nullable, 2, false, true))
					if nullable {
						if err == nil {
							c.Close()
							t.Fatal("nullable target admitted for nonnullable source")
						}
						return
					}
					if err != nil {
						t.Fatal(err)
					}
					defer c.Close()
					in, err := Instantiate(c, InstantiateOptions{})
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					for selector := uint64(0); selector < 6; selector++ {
						got, err := in.Invoke("run", selector, 7, 3)
						if selector == 0 {
							var trap *TrapError
							if !errors.As(err, &trap) || trap.Code != TrapNullReference {
								t.Fatalf("null: %v", err)
							}
							continue
						}
						match := target == 0 && (selector == 2 || selector == 3) || target == 1 && selector == 3 || target == 2 && selector == 4 || target == 0x6c && selector == 1
						want := uint64(49 * 3)
						if match == (sub == 24) {
							want = 48 * 3
						}
						if err != nil || len(got) != 1 || got[0] != want {
							t.Fatalf("input%d: %v, %v; want %d", selector, got, err, want)
						}
					}
				})
			}
		}
	}
}

func TestGCBranchCastLargeTypeIndex(t *testing.T) {
	requireCompleteCore3Backend(t)
	for _, target := range []uint32{4095, 4096} {
		t.Run(fmt.Sprint(target), func(t *testing.T) {
			types := make([][]byte, target+2)
			for i := uint32(0); i <= target; i++ {
				types[i] = []byte{0x5f, 0}
			}
			types[target+1] = wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})
			body := []byte{0x02, 0x6d, 0xfb, 1}
			body = append(body, wasmtest.ULEB(target)...)
			body = append(body, 0xfb, 24, 3, 0, 0x6d)
			body = append(body, wasmtest.SLEB32(int32(target))...)
			body = append(body, 0x0b, 0x1a, 0x41, 1, 0x0b)
			data := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(types...)),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(target+1))),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
			)
			c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			in, err := Instantiate(c, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			if got, err := in.Invoke("run"); err != nil || len(got) != 1 || got[0] != 1 {
				t.Fatalf("run = %v, %v", got, err)
			}
		})
	}
}

func TestGCBranchCastForeignReference(t *testing.T) {
	requireCompleteCore3Backend(t)
	for _, sub := range []byte{24, 25} {
		for _, target := range []byte{0, 0x6c, 0x71} {
			for _, nullable := range []bool{false, true} {
				t.Run(fmt.Sprintf("op%d/target%x/null%t", sub, target, nullable), func(t *testing.T) {
					flags := byte(1)
					if nullable {
						flags |= 2
					}
					body := []byte{1, 1, 0x7f, // local fallthrough flag
						0x02, 0x6e, 0x20, 0, 0xfb, 26, // block anyref; any.convert_extern
						0xfb, sub, flags, 0, 0x6e, target,
						0x41, 1, 0x21, 1, 0x0b,
						0xfb, 27, 0x20, 1, 0x0b} // extern.convert_any; flag
					data := wasmtest.Module(
						wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 0}, []byte{0x60, 1, 0x6f, 2, 0x6f, 0x7f})),
						wasmtest.Section(3, wasmtest.Vec([]byte{1})),
						wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
						wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
					)
					c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
					if err != nil {
						t.Fatal(err)
					}
					defer c.Close()
					in, err := Instantiate(c, InstantiateOptions{})
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					for i := 0; i < 16; i++ {
						token := uint64(0)
						if i != 0 {
							ref, err := in.NewExternRef(i)
							if err != nil {
								t.Fatal(err)
							}
							token = ValueExternRef(ref).Bits()
						}
						match := token == 0 && nullable
						wantFlag := uint64(1)
						if match == (sub == 24) {
							wantFlag = 0
						}
						got, err := in.Invoke("run", token)
						if err != nil || len(got) != 2 || got[0] != token || got[1] != wantFlag {
							t.Fatalf("input%d: %v, %v; want [%d %d]", i, got, err, token, wantFlag)
						}
					}
				})
			}
		}
	}
}

func TestGCBranchCastOnlyStackRoot(t *testing.T) {
	requireCompleteCore3Backend(t)
	for _, sub := range []byte{24, 25} {
		for _, target := range []byte{0, 2, 0x6c, 0x71} {
			for _, profile := range []GCProfile{GCProfileThroughput, GCProfileTiny} {
				t.Run(fmt.Sprintf("op%d/target%x/profile%d", sub, target, profile), func(t *testing.T) {
					// run has only an i32 parameter and no locals or Wasm caller.
					// The branch result is the object's sole root at the next allocation.
					body := []byte{0x02, 0x6d,
						0x20, 0, 0x20, 0, 0xfb, 0, 1,
						0xfb, sub, 1, 0, 0x6d, target, 0x0b,
						0xfb, 1, 0, 0x1a,
						0xfb, 22, 0, 0xfb, 2, 0, 0, 0x0b}
					data := wasmtest.Module(
						wasmtest.Section(1, wasmtest.Vec(
							[]byte{0x50, 0, 0x5f, 1, 0x7f, 0},
							[]byte{0x4f, 1, 0, 0x5f, 2, 0x7f, 0, 0x7f, 0},
							[]byte{0x5e, 0x7f, 1},
							wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
						)),
						wasmtest.Section(3, wasmtest.Vec([]byte{3})),
						wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
						wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
					)
					c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
					if err != nil {
						t.Fatal(err)
					}
					defer c.Close()
					in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{Profile: profile, CollectEveryAlloc: true, TinyCollectEveryAlloc: true, VerifyAfterCollect: true, StressNurseryBytes: 64, TinyHeapBytes: 64, TinyBlockBytes: 16}})
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					for value := uint64(1); value <= 32; value++ {
						if got, err := in.Invoke("run", value); err != nil || len(got) != 1 || got[0] != value {
							t.Fatalf("run(%d) = %v, %v", value, got, err)
						}
					}
					if s := in.gc.Stats(); s.MinorCollections+s.FullCollections == 0 {
						t.Fatal("no collections")
					}
					if err := in.CollectGC(); err != nil {
						t.Fatal(err)
					}
					if live := in.gc.Stats().LiveObjects; live != 0 {
						t.Fatalf("retained %d objects", live)
					}
				})
			}
		}
	}
}
