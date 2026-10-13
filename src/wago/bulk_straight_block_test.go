//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"errors"
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func bulkStraightModule(wide, shared bool, index uint32) []byte {
	addr, c := wasm.I32, byte(0x41)
	flags := byte(1)
	if wide {
		addr, c = wasm.I64, 0x42
		flags |= 4
	}
	if shared {
		flags |= 2
	}
	memories := [][]byte{{1, 1, 2}, {1, 1, 2}}
	memories[index] = []byte{flags, 1, 2}
	memarg := []byte{0, 0}
	if index != 0 {
		memarg = []byte{64, byte(index), 0}
	}
	var functions, bodies, exports [][]byte
	exports = append(exports, wasmtest.ExportEntry("memory", 2, index))
	for i, name := range []string{"dynamic", "constant", "load_before_fill", "trap_before_fill", "fill_before_trap"} {
		prefix := []byte{0x20, 3}
		if name == "load_before_fill" {
			prefix = []byte{0x20, 0, 0x2d}
			prefix = append(prefix, memarg...)
		}
		if name == "trap_before_fill" {
			prefix = append(prefix, 0x41, 0, 0x6e)
		}
		body := append(prefix, 0x02, 0x7f, 0x20, 0, 0x20, 1)
		if name == "constant" {
			body = append(body, c, 33)
		} else {
			body = append(body, 0x20, 2)
		}
		body = append(body, 0xfc, 11, byte(index))
		if name == "fill_before_trap" {
			body = append(body, 0x41, 1, 0x41, 0, 0x6e)
		} else {
			body = append(body, 0x41, 7)
		}
		body = append(body, 0x0b, 0x6a, 0x0b)
		functions = append(functions, wasmtest.ULEB(0))
		bodies = append(bodies, wasmtest.Code(body))
		exports = append(exports, wasmtest.ExportEntry(name, 0, uint32(i)))
	}
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{addr, wasm.I32, addr, wasm.I32}, []wasm.ValType{wasm.I32}))), wasmtest.Section(3, wasmtest.Vec(functions...)), wasmtest.Section(5, wasmtest.Vec(memories...)), wasmtest.Section(7, wasmtest.Vec(exports...)), wasmtest.Section(10, wasmtest.Vec(bodies...)))
}

func TestBulkStraightBlockPublicExecutionAndTraps(t *testing.T) {
	modes := []BoundsCheckMode{BoundsChecksExplicit}
	if guardPageBuilt {
		modes = append(modes, BoundsChecksSignalsBased)
	}
	for _, wide := range []bool{false, true} {
		for _, shared := range []bool{false, true} {
			for _, index := range []uint32{0, 1} {
				for _, enabled := range []bool{false, true} {
					for _, regABI := range []bool{false, true} {
						for _, mode := range modes {
							if shared && mode == BoundsChecksSignalsBased {
								continue
							}
							t.Run(fmt.Sprintf("wide=%t/shared=%t/index=%d/enabled=%t/regabi=%t/bounds=%v", wide, shared, index, enabled, regABI, mode), func(t *testing.T) {
								features := CoreFeaturesV3
								if shared {
									features |= CoreFeatureThreads
								}
								c, err := NewRuntimeConfig().WithCoreFeatures(features).WithBoundsChecks(mode).WithOptimization("straight-line-block", enabled).WithOptimization("reg-abi", regABI).Compile(bulkStraightModule(wide, shared, index))
								if err != nil {
									t.Fatal(err)
								}
								defer c.Close()
								for _, compiled := range []*Compiled{c, publicArtifactRoundTrip(t, c)} {
									in, err := Instantiate(compiled)
									if err != nil {
										t.Fatal(err)
									}
									defer in.Close()
									mem, err := in.ExportedMemory("memory")
									if err != nil {
										t.Fatal(err)
									}
									bytes := mem.UnsafeBytes()
									for _, length := range []uint64{0, 1, 7, 8, 16, 31, 33, 64, 65, 128, 129, 257} {
										for i := 0; i < 300; i++ {
											bytes[i] = 0x5a
										}
										got, err := in.Invoke("dynamic", 3, 0x1ff, length, 123)
										if err != nil || len(got) != 1 || got[0] != 130 {
											t.Fatalf("length=%d: %v,%v", length, got, err)
										}
										for i := 0; i < 300; i++ {
											want := byte(0x5a)
											if uint64(i) >= 3 && uint64(i) < 3+length {
												want = 0xff
											}
											if bytes[i] != want {
												t.Fatalf("length=%d byte[%d]=%x want=%x", length, i, bytes[i], want)
											}
										}
									}
									got, err := in.Invoke("constant", 3, 0x1ff, 0, 123)
									if err != nil || len(got) != 1 || got[0] != 130 {
										t.Fatalf("constant: %v,%v", got, err)
									}
									bytes[3] = 0x5a
									got, err = in.Invoke("load_before_fill", 3, 0x1ff, 33, 0)
									if err != nil || len(got) != 1 || got[0] != 97 {
										t.Fatalf("load snapshot: %v,%v", got, err)
									}
									for _, test := range []struct {
										name string
										want byte
									}{{"trap_before_fill", 0x5a}, {"fill_before_trap", 0xff}} {
										bytes[3] = 0x5a
										_, err = in.Invoke(test.name, 3, 0x1ff, 33, 123)
										var trap *TrapError
										if !errors.As(err, &trap) || trap.Code != TrapDivZero {
											t.Fatalf("%s: %v", test.name, err)
										}
										if bytes[3] != test.want {
											t.Fatalf("%s: byte=%x want=%x", test.name, bytes[3], test.want)
										}
									}
									for _, test := range []struct {
										address, length uint64
										trap            bool
									}{{65535, 1, false}, {65536, 0, false}, {65536, 1, true}, {65537, 0, true}, {65535, 2, true}, {^uint64(0), 1, true}, {0, ^uint64(0), true}} {
										_, err := in.Invoke("dynamic", test.address, 0x1ff, test.length, 123)
										if !test.trap {
											if err != nil {
												t.Fatal(err)
											}
											continue
										}
										var trap *TrapError
										if !errors.As(err, &trap) || trap.Code != TrapLinMemOutOfBounds {
											t.Fatalf("address=%x length=%x: %v", test.address, test.length, err)
										}
									}
								}
							})
						}
					}
				}
			}
		}
	}
}
