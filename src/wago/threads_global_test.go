//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"fmt"
	"sync"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func threadsMutableGlobalModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "counter", wasm.I32, true))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(5, wasmtest.Vec([]byte{3, 1, 1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("increment", 0, 0), wasmtest.ExportEntry("counter", 3, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x02, 0x40, 0x03, 0x40,
			0x20, 0, 0x45, 0x0d, 1,
			0x23, 0, 0x41, 1, 0x6a, 0x24, 0,
			0x41, 0, 0x23, 0, 0x36, 2, 0,
			0x20, 0, 0x41, 1, 0x6b, 0x21, 0, 0x0c, 0,
			0x0b, 0x0b, 0x23, 0, 0x0b,
		}))),
	)
}

func TestThreadsSharedMutableGlobalConcurrentImportersAndCodec(t *testing.T) {
	for _, cache := range []bool{false, true} {
		for _, regABI := range []bool{false, true} {
			t.Run(fmt.Sprintf("cache=%t/regabi=%t", cache, regABI), func(t *testing.T) {
				cfg := NewRuntimeConfig().WithFeature(CoreFeatureThreads, true).WithBoundsChecks(BoundsChecksExplicit).
					WithOptimization("shared-memory-base", cache).WithOptimization("reg-abi", regABI)
				compiled, err := cfg.Compile(threadsMutableGlobalModule())
				if err != nil {
					t.Fatal(err)
				}
				defer compiled.Close()
				for _, c := range []*Compiled{compiled} {
					global := NewGlobalI32(17, true)
					defer global.Close()
					const workers, iterations = 8, 10000
					instances := make([]*Instance, workers)
					for index := range instances {
						instances[index], err = Instantiate(c, NewImports().Global("env", "counter", global))
						if err != nil {
							t.Fatal(err)
						}
						defer instances[index].Close()
						if instances[index].usesPrivateThreadedExecution() {
							t.Fatal("mutable global importer retained a private native lease")
						}
					}
					start := make(chan struct{})
					failures := make(chan error, workers)
					var group sync.WaitGroup
					for _, in := range instances {
						group.Add(1)
						go func(in *Instance) {
							defer group.Done()
							<-start
							_, err := in.Invoke("increment", iterations)
							failures <- err
						}(in)
					}
					close(start)
					group.Wait()
					close(failures)
					for err := range failures {
						if err != nil {
							t.Fatal(err)
						}
					}
					if got := global.Get(); got != 17+workers*iterations {
						t.Fatalf("concurrent counter = %d; want %d", got, 17+workers*iterations)
					}
					reexport, err := instances[0].ExportedGlobalObject("counter")
					if err != nil || reexport != global {
						t.Fatalf("global reexport: %p, %v", reexport, err)
					}
					if err := global.Set(99); err != nil {
						t.Fatal(err)
					}
					got, err := instances[0].Invoke("increment", 1)
					if err != nil || len(got) != 1 || got[0] != 100 {
						t.Fatalf("host mutation handoff: %v, %v", got, err)
					}
					if err := global.Close(); err == nil {
						t.Fatal("global closed while importers remained live")
					}
				}
			})
		}
	}
}
