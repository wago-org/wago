//go:build ((linux && (amd64 || arm64)) || (darwin && arm64)) && !tinygo

package wago

import (
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func indexedWaitOffset(value uint64) []byte {
	var out []byte
	for {
		b := byte(value & 127)
		value >>= 7
		if value == 0 {
			return append(out, b)
		}
		out = append(out, b|128)
	}
}

// A shared selected memory and an unshared memory of the other address width
// keep the helper's address type and memory index independently observable.
func indexedWaitModule(address64 bool, index, sub byte, offset uint64, imported bool) []byte {
	address, expected := wasm.I32, wasm.I32
	limits, other := byte(3), byte(5)
	if address64 {
		address, limits, other = wasm.I64, 7, 1
	}
	align := byte(2)
	if sub == 2 {
		expected, align = wasm.I64, 3
	}
	memories := [][]byte{{limits, 1, 2}, {other, 0, 1}}
	if index != 0 {
		memories[0], memories[1] = memories[1], memories[0]
	}
	op := append([]byte{0xfe, sub, align | 0x40, index}, indexedWaitOffset(offset)...)
	operands := []byte{0x20, 0, 0x20, 1}
	if sub != 0 {
		operands = append(operands, 0x20, 2)
	}
	run := []byte{1, 1, 0x7e, 0x20, 3, 0x42, 1, 0x7c, 0x21, 4, 0x20, 3, 0x42, 2, 0x7c}
	run = append(run, operands...)
	run = append(run, op...)
	run = append(run, 0x20, 4, 0x0b)
	vector := []byte{0xfd, 12, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
	vector = append(vector, operands...)
	vector = append(vector, op...)
	vector = append(vector, 0x0b)
	// Runtime expected value = base | (counter & mask); dynamic timeout0 must
	// retain the queue helper whenever the comparison matches.
	loop := []byte{2, 1, 0x7f, 1, 0x7f, 0x02, 0x40, 0x03, 0x40, 0x20, 5, 0x20, 0, 0x4f, 0x0d, 1, 0x20, 6, 0x20, 1, 0x20, 5}
	and, or := byte(0x71), byte(0x72)
	if sub == 2 {
		loop = append(loop, 0xad)
		and, or = 0x83, 0x84
	}
	loop = append(loop, 0x20, 3, and, 0x20, 2, or)
	if sub != 0 {
		loop = append(loop, 0x20, 4)
	}
	loop = append(loop, op...)
	loop = append(loop, 0x6a, 0x21, 6, 0x20, 5, 0x41, 1, 0x6a, 0x21, 5, 0x0c, 0, 0x0b, 0x0b, 0x20, 6, 0x0b)
	raw := func(body []byte) []byte { return append(wasmtest.ULEB(uint32(len(body))), body...) }
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(
		wasmtest.FuncType([]wasm.ValType{address, expected, wasm.I64, wasm.I64}, []wasm.ValType{wasm.I64, wasm.I32, wasm.I64}),
		wasmtest.FuncType([]wasm.ValType{wasm.I32, address, expected, expected, wasm.I64}, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType([]wasm.ValType{address, expected, wasm.I64}, []wasm.ValType{wasm.V128, wasm.I32})))}
	if imported {
		var entries [][]byte
		for i, memory := range memories {
			entry := append(wasmtest.Name("env"), wasmtest.Name(fmt.Sprintf("memory%d", i))...)
			entries = append(entries, append(append(entry, 2), memory...))
		}
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(entries...)))
	}
	sections = append(sections, wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{1}, []byte{2})))
	if !imported {
		sections = append(sections, wasmtest.Section(5, wasmtest.Vec(memories...)))
	}
	sections = append(sections, wasmtest.Section(7, wasmtest.Vec(
		wasmtest.ExportEntry("run", 0, 0), wasmtest.ExportEntry("loop", 0, 1), wasmtest.ExportEntry("vector", 0, 2),
		wasmtest.ExportEntry("memory", 2, uint32(index)), wasmtest.ExportEntry("memory0", 2, 0), wasmtest.ExportEntry("memory1", 2, 1))),
		wasmtest.Section(10, wasmtest.Vec(raw(run), raw(loop), wasmtest.Code(vector))))
	return wasmtest.Module(sections...)
}

func indexedWaitConfig() *RuntimeConfig {
	return NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit)
}

func TestIndexedAtomicWaitResultsAndTraps(t *testing.T) {
	for _, address64 := range []bool{false, true} {
		for _, index := range []byte{0, 1} {
			for _, sub := range []byte{0, 1, 2} {
				for _, offset := range []uint64{0, 8, 0xfffffff8, ^uint64(0) - 7, ^uint64(0) - 2} {
					if !address64 && offset > 0xffffffff {
						continue
					}
					t.Run(fmt.Sprintf("addr64=%t/index=%d/op=%d/offset=%x", address64, index, sub, offset), func(t *testing.T) {
						c, err := indexedWaitConfig().Compile(indexedWaitModule(address64, index, sub, offset, false))
						if err != nil {
							t.Fatal(err)
						}
						defer c.Close()
						in, err := Instantiate(c)
						if err != nil {
							t.Fatal(err)
						}
						defer in.Close()
						size := uint64(4)
						if sub == 2 {
							size = 8
						}
						for _, input := range []uint64{0, 1, 4, 8, 65528, 65532, 65536, 65537, 1 << 32, 1<<32 | 8, ^uint64(0), ^uint64(0) - 7} {
							address := input
							if !address64 {
								address = uint64(uint32(address))
							}
							end := address + offset
							var wantTrap TrapCode
							if end&(size-1) != 0 {
								wantTrap = TrapAtomicUnaligned
							} else if end < address || end > 65536 || size > 65536-end {
								wantTrap = TrapLinMemOutOfBounds
							}
							for _, expected := range []uint64{0, 1, 1 << 32, ^uint64(0)} {
								got, err := in.Invoke("run", input, expected, 0, 1<<40)
								if wantTrap != 0 {
									var trap *TrapError
									if !errors.As(err, &trap) || trap.Code != wantTrap {
										t.Fatalf("addr%x value%x: %v;want trap%d", input, expected, err, wantTrap)
									}
									continue
								}
								want := uint64(0)
								if sub != 0 {
									compare := expected
									if sub == 1 {
										compare = uint64(uint32(compare))
									}
									want = 2
									if compare != 0 {
										want = 1
									}
								}
								if err != nil || len(got) != 3 || got[0] != (1<<40)+2 || got[1] != want || got[2] != (1<<40)+1 {
									t.Fatalf("addr%x value%x: %x,%v;want result%d", input, expected, got, err, want)
								}
							}
						}
					})
				}
			}
		}
	}
}

func TestIndexedAtomicWaitVectorCodecAndOptions(t *testing.T) {
	for _, address64 := range []bool{false, true} {
		for _, index := range []byte{0, 1} {
			for _, sub := range []byte{0, 1, 2} {
				for _, cache := range []bool{false, true} {
					for _, regABI := range []bool{false, true} {
						c, err := indexedWaitConfig().WithOptimization("shared-memory-base", cache).WithOptimization("reg-abi", regABI).Compile(indexedWaitModule(address64, index, sub, 0, false))
						if err != nil {
							t.Fatal(err)
						}
						defer c.Close()
						loaded := publicArtifactRoundTrip(t, c)
						defer loaded.Close()
						for _, compiled := range []*Compiled{c, loaded} {
							in, err := Instantiate(compiled)
							if err != nil {
								t.Fatal(err)
							}
							for _, expected := range []uint64{1, 0} {
								want := uint64(0)
								if sub != 0 {
									want = 2 - expected
								}
								got, err := in.Invoke("vector", 0, expected, 0)
								if err != nil || len(got) != 3 || got[0] != 0x0706050403020100 || got[1] != 0x0f0e0d0c0b0a0908 || got[2] != want {
									in.Close()
									t.Fatalf("addr64%t/index%d/op%d/cache%t/regABI%t: %x,%v", address64, index, sub, cache, regABI, got, err)
								}
							}
							in.Close()
						}
					}
				}
			}
		}
	}
}

func BenchmarkIndexedAtomicWait(b *testing.B) {
	const iterations = 1024
	for _, address64 := range []bool{false, true} {
		for _, index := range []byte{0, 1} {
			for _, sub := range []byte{1, 2} {
				name := fmt.Sprintf("addr64=%t/index=%d/op=%d", address64, index, sub)
				b.Run(name, func(b *testing.B) {
					cfg, module := indexedWaitConfig(), indexedWaitModule(address64, index, sub, 0, false)
					b.Run("compile", func(b *testing.B) {
						b.ReportAllocs()
						for n := 0; n < b.N; n++ {
							c, err := cfg.Compile(module)
							if err != nil {
								b.Fatal(err)
							}
							c.Close()
						}
					})
					c, err := cfg.Compile(module)
					if err != nil {
						b.Fatal(err)
					}
					defer c.Close()
					in, err := Instantiate(c)
					if err != nil {
						b.Fatal(err)
					}
					defer in.Close()
					for _, path := range []struct {
						name       string
						base, mask uint64
						want       uint64
					}{{"mismatch", 1, 0, iterations}, {"matching-helper", 0, 0, 2 * iterations}, {"mixed", 0, 1, 3 * iterations / 2}} {
						b.Run(path.name, func(b *testing.B) {
							b.ReportAllocs()
							for n := 0; n < b.N; n++ {
								got, err := in.Invoke("loop", iterations, 0, path.base, path.mask, 0)
								if err != nil || len(got) != 1 || got[0] != path.want {
									b.Fatalf("%x,%v;want%d", got, err, path.want)
								}
							}
							b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/iterations, "ns/wait")
							b.ReportMetric(float64(len(c.code)), "code-B")
						})
					}
				})
			}
		}
	}
}
