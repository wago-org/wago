//go:build linux && riscv64 && !tinygo && !wago_target_tinygo && !wago_no_bulk_memory_operations && !wago_no_reference_types

package wago

import (
	"context"
	"fmt"
	"testing"
	"unsafe"

	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

func TestRV64CompactExternBulkScratchPrivate(t *testing.T) {
	for _, vector := range []bool{false, true} {
		c, err := NewRuntimeConfig().Compile(rv64CompactExternBulkModuleWith(vector, false, false, 12, 16))
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()

		for artifact, candidate := range []*Compiled{c} {
			for _, profile := range []gc.Profile{gc.ProfileThroughput, gc.ProfileTiny} {
				t.Run(fmt.Sprintf("vector%t/artifact%t/profile%d", vector, artifact != 0, profile), func(t *testing.T) {
					rt := NewRuntime()
					defer rt.Close()
					mod, err := rt.Module(candidate)
					if err != nil {
						t.Fatal(err)
					}
					defer mod.Close()
					in, err := rt.Instantiate(context.Background(), mod, WithGC(GCConfig{Profile: profile, CollectEveryAlloc: true, VerifyAfterCollect: true}))
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					foreign, err := rt.NewExternRef("bulk scratch")
					if err != nil {
						t.Fatal(err)
					}
					if _, err = in.Invoke("fill", 0, foreign.token, 12); err != nil {
						t.Fatal(err)
					}
					if _, err = in.Invoke("copy", 0, 0, 12); err != nil {
						t.Fatal(err)
					}
					fn, err := in.WasmFunc("copy")
					if err != nil {
						t.Fatal(err)
					}
					session, err := fn.OpenSession()
					if err != nil {
						t.Fatal(err)
					}
					allocs := testing.AllocsPerRun(1000, func() {
						if _, err := session.Invoke3(0, 0, 12); err != nil {
							panic(err)
						}
					})
					session.Close()
					if allocs != 0 {
						t.Fatal("warm copy Go allocations", allocs)
					}
					state, _ := compactExternCollectors.Load(in.gc)
					if cap(state.bulk) != 12 {
						t.Fatal("copy scratch capacity", cap(state.bulk))
					}
					for _, v := range state.bulk[:cap(state.bulk)] {
						if v != (compactExternPreparedWord{}) {
							t.Fatal("copy scratch retained a prepared root/pin")
						}
					}
					if _, err := in.Invoke("fill", 0, 0, 12); err != nil {
						t.Fatal(err)
					}
					if err := in.CollectGC(); err != nil {
						t.Fatal(err)
					}
					if !rt.ReleaseExternRef(foreign) {
						t.Fatal("bulk scratch retained a foreign pin")
					}
					t.Logf("copy scratch=%d Go bytes, reused for 1000 calls", cap(state.bulk)*int(unsafe.Sizeof(compactExternPreparedWord{})))
				})
			}
		}
	}
}
