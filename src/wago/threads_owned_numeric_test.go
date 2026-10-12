//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"context"
	"encoding/binary"
	"fmt"
	"testing"
)

func TestThreadsOwnedTypedCallbackGrowthPublicationAndCodec(t *testing.T) {
	for _, cache := range []bool{false, true} {
		for _, regABI := range []bool{false, true} {
			t.Run(fmt.Sprintf("cache=%t/regabi=%t", cache, regABI), func(t *testing.T) {
				cfg := NewRuntimeConfig().WithFeature(CoreFeatureThreads, true).WithBoundsChecks(BoundsChecksExplicit).
					WithOptimization("shared-memory-base", cache).WithOptimization("reg-abi", regABI)
				rt := NewRuntime(WithRuntimeConfig(cfg))
				defer rt.Close()
				compiled, err := cfg.Compile(threadsHostModule(true))
				if err != nil {
					t.Fatal(err)
				}
				defer compiled.Close()
				growModule, err := rt.Compile(threadsGrowModule(true, 1, 3))
				if err != nil {
					t.Fatal(err)
				}
				defer growModule.Close()
				for _, c := range []*Compiled{compiled, publicArtifactRoundTrip(t, compiled)} {
					var in, grower *Instance
					memory, err := NewSharedMemory(1, 3)
					if err != nil {
						t.Fatal(err)
					}
					defer memory.Close()
					called := 0
					owner, err := rt.NewHostFuncRef(func(value int32) int32 {
						called++
						if called == 1 {
							if got, err := grower.Invoke("grow", 1); err != nil || len(got) != 1 || got[0] != 1 {
								panic(HostTrap{Err: fmt.Errorf("typed callback growth: %v, %v", got, err)})
							}
							if _, err := in.ExportedFunc("run"); err != nil {
								panic(HostTrap{Err: err})
							}
						}
						bytes := memory.UnsafeBytes()
						if len(bytes) != 131072 {
							panic(HostTrap{Err: fmt.Errorf("typed callback memory extent = %d", len(bytes))})
						}
						binary.LittleEndian.PutUint32(bytes, uint32(value)+5)
						return value * 3
					}, FuncSig{Params: []ValType{ValI32}, Results: []ValType{ValI32}})
					if err != nil {
						t.Fatal(err)
					}
					defer owner.Close()
					mod, err := rt.Module(c)
					if err != nil {
						t.Fatal(err)
					}
					defer mod.Close()
					in, err = rt.Instantiate(context.Background(), mod, WithImports(NewImports().Function("env", "host", owner).Memory("env", "memory", memory)))
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					grower, err = rt.Instantiate(context.Background(), growModule, WithImports(NewImports().Memory("env", "memory", memory)))
					if err != nil {
						t.Fatal(err)
					}
					defer grower.Close()
					if !in.usesPrivateThreadedExecution() {
						t.Fatal("owned numeric root was already published before callback")
					}
					for _, value := range []uint64{7, 11} {
						got, err := in.Invoke("run", value)
						if err != nil || len(got) != 1 || got[0] != 4*value+7 {
							t.Fatalf("owned typed shared continuation: %v, %v", got, err)
						}
					}
					if called != 2 {
						t.Fatalf("typed callback count = %d", called)
					}
					if in.usesPrivateThreadedExecution() {
						t.Fatal("published numeric root retained its private lease")
					}
					if err := owner.Close(); err == nil {
						t.Fatal("typed owner closed while importer was live")
					}
				}
			})
		}
	}
}
