//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"errors"
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func straightBlockModule(shared bool) []byte {
	functions := [][]byte{
		// Keep the earlier local.get value even when the block changes that local.
		{0x20, 0, 0x02, 0x7f, 0x41, 9, 0x21, 0, 0x41, 7, 0x0b, 0x6a, 0x0b},
		// Realize an older memory read before a store inside the block.
		{0x41, 0, 0x28, 0, 0, 0x02, 0x7f, 0x41, 0, 0x41, 8, 0x36, 0, 0, 0x41, 7, 0x0b, 0x6a, 0x0b},
		// A deferred divide before the block must trap before its store.
		{0x41, 1, 0x41, 0, 0x6e, 0x02, 0x7f, 0x41, 0, 0x41, 8, 0x36, 0, 0, 0x41, 7, 0x0b, 0x6a, 0x0b},
		// The store must remain visible when a later divide in the block traps.
		{0x41, 5, 0x02, 0x7f, 0x41, 0, 0x41, 8, 0x36, 0, 0, 0x41, 1, 0x41, 0, 0x6e, 0x0b, 0x6a, 0x0b},
		// Preserve a detached older operand around if arms containing straight blocks.
		{0x41, 42, 0x20, 0, 0x04, 0x7f, 0x02, 0x7f, 0x41, 7, 0x0b, 0x05, 0x02, 0x7f, 0x41, 9, 0x0b, 0x0b, 0x6a, 0x0b},
	}
	names := []string{"local_snapshot", "load_snapshot", "trap_before_store", "store_before_trap", "if_prefix"}
	var funcs, bodies, exports [][]byte
	for i, body := range functions {
		funcs = append(funcs, wasmtest.ULEB(0))
		bodies = append(bodies, wasmtest.Code(body))
		exports = append(exports, wasmtest.ExportEntry(names[i], 0, uint32(i)))
	}
	flags := byte(1)
	if shared {
		flags = 3
	}
	exports = append(exports, wasmtest.ExportEntry("memory", 2, 0))
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))), wasmtest.Section(3, wasmtest.Vec(funcs...)), wasmtest.Section(5, wasmtest.Vec([]byte{flags, 1, 2})), wasmtest.Section(7, wasmtest.Vec(exports...)), wasmtest.Section(10, wasmtest.Vec(bodies...)))
}

func TestStraightBlockPublicValueAndTrapOrder(t *testing.T) {
	modes := []BoundsCheckMode{BoundsChecksExplicit}
	if guardPageBuilt {
		modes = append(modes, BoundsChecksSignalsBased)
	}
	for _, shared := range []bool{false, true} {
		features := CoreFeaturesV2
		if shared {
			features |= CoreFeatureThreads
		}
		for _, enabled := range []bool{false, true} {
			for _, regABI := range []bool{false, true} {
				for _, mode := range modes {
					if shared && mode == BoundsChecksSignalsBased {
						continue
					}
					t.Run(fmt.Sprintf("shared=%t/enabled=%t/regabi=%t/bounds=%v", shared, enabled, regABI, mode), func(t *testing.T) {
						c, err := NewRuntimeConfig().WithCoreFeatures(features).WithOptimization("straight-line-block", enabled).WithOptimization("reg-abi", regABI).WithBoundsChecks(mode).Compile(straightBlockModule(shared))
						if err != nil {
							t.Fatal(err)
						}
						defer c.Close()
						for _, compiled := range []*Compiled{c} {
							in, err := Instantiate(compiled)
							if err != nil {
								t.Fatal(err)
							}
							defer in.Close()
							mem, err := in.ExportedMemory("memory")
							if err != nil {
								t.Fatal(err)
							}
							got, err := in.Invoke("local_snapshot", 123)
							if err != nil || len(got) != 1 || got[0] != 130 {
								t.Fatalf("local snapshot: %v,%v", got, err)
							}
							for _, condition := range []uint64{0, 1} {
								want := uint64(51)
								if condition != 0 {
									want = 49
								}
								got, err := in.Invoke("if_prefix", condition)
								if err != nil || len(got) != 1 || got[0] != want {
									t.Fatalf("if prefix: %v,%v want=%d", got, err, want)
								}
							}
							mem.UnsafeBytes()[0] = 99
							got, err = in.Invoke("load_snapshot", 0)
							if err != nil || len(got) != 1 || got[0] != 106 {
								t.Fatalf("load snapshot: %v,%v", got, err)
							}
							for _, test := range []struct {
								name string
								want byte
							}{{"trap_before_store", 99}, {"store_before_trap", 8}} {
								mem.UnsafeBytes()[0] = 99
								_, err = in.Invoke(test.name, 0)
								var trap *TrapError
								if !errors.As(err, &trap) || trap.Code != TrapDivZero {
									t.Fatalf("%s: %v", test.name, err)
								}
								if got := mem.UnsafeBytes()[0]; got != test.want {
									t.Fatalf("%s: memory=%d want=%d", test.name, got, test.want)
								}
							}
						}
					})
				}
			}
		}
	}
}
