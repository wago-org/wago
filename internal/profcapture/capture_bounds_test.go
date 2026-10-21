//go:build wago_profile

package profcapture

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/wago-org/wago"
)

func TestCaptureExplicitBoundsOverrideEnvironment(t *testing.T) {
	t.Setenv("WAGO_BOUNDS", "signals")
	for _, tc := range []struct {
		name, mode, bounds string
		config             *wago.RuntimeConfig
	}{
		{name: "public", mode: "public", bounds: "explicit"},
		{name: "prepared", mode: "prepared", bounds: "explicit"},
		{name: "application", mode: "application", bounds: "explicit"},
		{name: "supplied configuration", mode: "application", bounds: "signals", config: wago.NewRuntimeConfig().WithBoundsChecks(wago.BoundsChecksExplicit)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "capture")
			o := Options{Out: out, Backend: "none", Phase: "execute", Mode: tc.mode, Iterations: 1, Bounds: tc.bounds, Rate: 99}
			var err error
			if tc.mode == "application" {
				err = RunWithHarness(o, Harness{ID: "test", Contract: "identity37", WorkUnit: "call", Wasm: fixtureModule(), RuntimeConfig: tc.config, Execute: func(in *wago.Instance) error {
					result, err := in.Invoke("identity", 37)
					if err != nil || len(result) != 1 || result[0] != 37 {
						return fmt.Errorf("identity(37): %v, %v", result, err)
					}
					return nil
				}})
			} else {
				err = Run(o, Workload{ID: "test", Calls: []Call{{Export: "identity", Args: []uint64{37}, Want: []uint64{37}}}}, fixtureModule())
			}
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(out, "manifest.json"))
			if err != nil {
				t.Fatal(err)
			}
			var manifest Manifest
			if err = json.Unmarshal(data, &manifest); err != nil {
				t.Fatal(err)
			}
			if manifest.Config["bounds"] != "explicit" {
				t.Fatalf("requested explicit bounds, got %v", manifest.Config["bounds"])
			}
		})
	}
}
