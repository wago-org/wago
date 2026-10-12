//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func threadsBulkMemoryModule(fill, imported bool, pages byte, length int) []byte {
	return threadsBulkMemoryModuleForAddress(fill, imported, pages, length, false)
}

func threadsBulkMemoryModuleForAddress(fill, imported bool, pages byte, length int, address64 bool) []byte {
	body := []byte{0x20, 0, 0x20, 1}
	if length < 0 {
		body = append(body, 0x20, 2)
	} else {
		constantOp := byte(0x41)
		if address64 {
			constantOp = 0x42
		}
		body = append(body, constantOp)
		body = append(body, wasmtest.SLEB32(int32(length))...)
	}
	if fill {
		body = append(body, 0xfc, 11, 0)
	} else {
		body = append(body, 0xfc, 10, 0, 0)
	}
	body = append(body, 0x0b)
	addressType, limits := wasm.I32, byte(3)
	if address64 {
		addressType = wasm.I64
		limits = 7
	}
	secondType := addressType
	if fill {
		secondType = wasm.I32
	}
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{addressType, secondType, addressType}, nil)))}
	if imported {
		entry := append(wasmtest.Name("env"), wasmtest.Name("memory")...)
		entry = append(entry, 2, limits, pages, pages)
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(entry)))
	}
	sections = append(sections, wasmtest.Section(3, wasmtest.Vec([]byte{0})))
	if !imported {
		sections = append(sections, wasmtest.Section(5, wasmtest.Vec([]byte{limits, pages, pages})))
	}
	sections = append(sections,
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("bulk", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	return wasmtest.Module(sections...)
}

func TestThreadsSharedBulkMemoryExecutionAndCodec(t *testing.T) {
	cfg := NewRuntimeConfig().WithFeature(CoreFeatureThreads, true).WithBoundsChecks(BoundsChecksExplicit)
	for _, fill := range []bool{false, true} {
		for _, imported := range []bool{false, true} {
			for _, pages := range []byte{0, 1} {
				for _, length := range []int{-1, 0, 1, 7, 16, 31, 32, 33, 63, 64, 65, 1024} {
					t.Run(fmt.Sprintf("fill=%t/imported=%t/pages=%d/length=%d", fill, imported, pages, length), func(t *testing.T) {
						c, err := cfg.Compile(threadsBulkMemoryModule(fill, imported, pages, length))
						if err != nil {
							t.Fatal(err)
						}
						defer c.Close()
						loaded := publicArtifactRoundTrip(t, c)
						for _, compiled := range []*Compiled{c, loaded} {
							var opts any = InstantiateOptions{}
							if imported {
								memory, err := NewSharedMemory(uint32(pages), uint32(pages))
								if err != nil {
									t.Fatal(err)
								}
								defer memory.Close()
								opts = testImports("env.memory", memory)
							}
							in, err := Instantiate(compiled, opts)
							if err != nil {
								t.Fatal(err)
							}
							defer in.Close()
							counts := []uint64{0}
							if length < 0 {
								counts = []uint64{0, 1, 7, 16, 33, 64, 65, 96, 127, 128, 1024, 65536, 0xffffffff, 1<<32 | 7}
							}
							for _, n := range counts {
								for _, pair := range [][2]uint64{{0, 0}, {1, 0}, {0, 1}, {7, 1}, {1, 7}, {65520, 0}, {0, 65520}, {65535, 65535}, {65536, 65536}, {65537, 0}, {0, 65537}, {0xffffffff, 0}, {0, 0xffffffff}, {1<<32 | 1, 1<<32 | 7}} {
									mem := in.mem()
									for i := range mem {
										mem[i] = byte(i*37 + 0x81)
									}
									want := bytes.Clone(mem)
									count := uint64(uint32(n))
									if length >= 0 {
										count = uint64(length)
									}
									dst, src := uint64(uint32(pair[0])), uint64(uint32(pair[1]))
									valid := dst+count <= uint64(len(mem)) && (fill || src+count <= uint64(len(mem)))
									if valid {
										if fill {
											for i := dst; i < dst+count; i++ {
												want[i] = byte(src)
											}
										} else {
											copy(want[dst:dst+count], want[src:src+count])
										}
									}
									_, err := in.Invoke("bulk", pair[0], pair[1], n)
									if valid && err != nil {
										t.Fatalf("dst=%x src/value=%x n=%x: %v", pair[0], pair[1], n, err)
									}
									if !valid {
										trap, ok := err.(*TrapError)
										if !ok || trap.Code != TrapLinMemOutOfBounds {
											t.Fatalf("dst=%x src/value=%x n=%x: want bounds trap, got %v", pair[0], pair[1], n, err)
										}
									}
									if !bytes.Equal(want, mem) {
										t.Fatalf("dst=%x src/value=%x n=%x: memory mismatch", pair[0], pair[1], n)
									}
								}
							}
						}
					})
				}
			}
		}
	}
}
