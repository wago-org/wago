//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"fmt"
	"testing"
)

func TestGCOptionPrivateLocalsRespectSourceLimit(t *testing.T) {
	source, _ := iteratorCarrierFixture(16, 0, 0)
	for _, disabled := range []string{"1", "0"} {
		for _, limit := range []uint32{4, 5, 6} {
			t.Run(fmt.Sprintf("disabled%s/limit%d", disabled, limit), func(t *testing.T) {
				t.Setenv("WAGO_AMD64_NO_GC_CARRIER_ELISION", disabled)
				c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompileWorkers(1).WithMaxFunctionLocals(limit), source)
				if err != nil {
					t.Fatalf("source has 4 declared locals: %v", err)
				}
				defer c.Close()
				in, err := Instantiate(c, InstantiateOptions{})
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				got, err := in.Invoke("run")
				if err != nil || len(got) != 1 || got[0] != 120 {
					t.Fatalf("got %v %v; want 120", got, err)
				}
			})
		}
	}
}
