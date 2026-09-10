//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package wago

import (
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Both controls enter the same host-capable export. The host returns the same
// increment as the guest control, and the final accumulator is checked.
func hostRoundtripLoopModule(b testing.TB, memories int) []byte {
	mem := wasmtest.ULEB(uint32(memories))
	for i := 0; i < memories; i++ {
		mem = append(mem, 1, 1, 2)
	}
	body := []byte{
		1, 1, 0x7f, // local sum: i32
		0x02, 0x40, 0x03, 0x40, // block done; loop next
		0x20, 0, 0x45, 0x0d, 1, // count == 0: branch done
		0x20, 1, 0x04, 0x7f, // if host, result i32
		0x20, 2, 0x10, 0, // step(sum)
		0x05, 0x20, 2, 0x41, 1, 0x6a, 0x0b, // else sum+1; end
		0x21, 2, // sum = result
		0x20, 0, 0x41, 1, 0x6b, 0x21, 0, // count--
		0x0c, 0, 0x0b, 0x0b, // branch next; end loop/block
		0x20, 2, 0x0b, // return sum
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(importEntry("env", "step", 0, 0))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(5, mem),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func BenchmarkHostRoundtripLoop(b *testing.B) {
	for _, memories := range []int{0, 1, 4} {
		b.Run(fmt.Sprintf("mem%d", memories), func(b *testing.B) {
			cfg := NewRuntimeConfig()
			if memories > 1 {
				if !SupportedFeatures().IsEnabled(CoreFeatureMultiMemory) {
					b.Skip("additional fixture requires multi-memory support")
				}
				cfg = cfg.WithCoreFeatures(CoreFeaturesV2 | CoreFeatureMultiMemory)
			}
			c, err := Compile(cfg, hostRoundtripLoopModule(b, memories))
			if err != nil {
				b.Fatal(err)
			}
			defer c.Close()
			for _, parallel := range []bool{false, true} {
				for _, host := range []int32{0, 1} {
					for _, count := range []int32{0, 1, 8, 64, 1024} {
						b.Run(fmt.Sprintf("parallel%t/host%d/n%d", parallel, host, count), func(b *testing.B) {
							workers := 1
							if parallel {
								workers = runtime.GOMAXPROCS(0)
							}
							instances := make([]*Instance, workers)
							for i := range instances {
								in, err := Instantiate(c, InstantiateOptions{Imports: Imports{"env.step": HostFunc(func(_ HostModule, p, r []uint64) { r[0] = p[0] + 1 })}})
								if err != nil {
									b.Fatal(err)
								}
								instances[i] = in
								defer in.Close()
								if _, err := in.Invoke("run", I32(count), I32(host)); err != nil {
									b.Fatal(err)
								}
							}
							invoke := func(in *Instance) {
								got, err := in.Invoke("run", I32(count), I32(host))
								if err != nil || len(got) != 1 || got[0] != uint64(count) {
									b.Fatalf("run = %v, %v; want %d", got, err, count)
								}
							}
							b.ReportAllocs()
							b.ResetTimer()
							if parallel {
								var next atomic.Uint32
								b.RunParallel(func(pb *testing.PB) {
									in := instances[next.Add(1)-1]
									for pb.Next() {
										invoke(in)
									}
								})
							} else {
								for i := 0; i < b.N; i++ {
									invoke(instances[0])
								}
							}
						})
					}
				}
			}
		})
	}
}
