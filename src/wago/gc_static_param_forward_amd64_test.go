//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestGCStaticParameterForwardRootsAndTraps(t *testing.T) {
	data, err := os.ReadFile("../../tests/fixtures/wasm/gc-static-param-forward.wasm")
	if err != nil {
		t.Fatal(err)
	}
	data = paddedReferenceTailRoots(t, data)
	for _, inline := range []bool{false, true} {
		for _, workers := range []int{1, 4} {
			t.Run(fmt.Sprintf("inline=%t/workers=%d", inline, workers), func(t *testing.T) {
				cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompileWorkers(workers).WithNativeStackBytes(512<<10).WithOptimization("inline", inline)
				c, err := Compile(cfg, data)
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				if a := c.GCNativeRootAdmission(); !a.Required || !a.Exact {
					t.Fatalf("exact root admission lost: %+v", a)
				}
				for _, profile := range directReferenceABIGCProfiles() {
					in, err := Instantiate(c, InstantiateOptions{GC: profile.gc})
					if err != nil {
						t.Fatal(err)
					}
					func() {
						defer in.Close()
						for _, entry := range []struct {
							name string
							want uint64
						}{{"run", 40}, {"pair", 40}, {"null", 7}, {"assigned", 91}, {"chain", 40}} {
							for range 3 {
								before := in.gc.Stats().FullCollections
								got, err := in.Invoke(entry.name)
								if err != nil || len(got) != 1 || got[0] != entry.want {
									t.Fatalf("%s/%s=%v,%v want%d", profile.name, entry.name, got, err, entry.want)
								}
								if profile.gc.CollectEveryAlloc && entry.name == "run" && in.gc.Stats().FullCollections-before < 4 {
									t.Fatal("hidden and returned references did not cross forced collections")
								}
								if err := in.CollectGC(); err != nil {
									t.Fatal(err)
								}
							}
						}
						if _, err := in.Invoke("cycle"); err == nil || !strings.Contains(err.Error(), "stack") {
							t.Fatalf("cyclic forwarding must reach the stack fence: %v", err)
						}
						for _, entry := range []string{"trap-null", "trap-type"} {
							if _, err := in.Invoke("reset"); err != nil {
								t.Fatal(err)
							}
							if _, err := in.Invoke(entry); err == nil || !strings.Contains(err.Error(), "cast") {
								t.Fatalf("%s must fail its dynamic cast: %v", entry, err)
							}
							got, err := in.Invoke("effects")
							if err != nil || len(got) != 1 || got[0] != 0 {
								t.Fatalf("%s reached the effectful target: %v,%v", entry, got, err)
							}
						}
					}()
				}
			})
		}
	}
}
