//go:build wago_profile && !tinygo

package profcapture

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"unsafe"

	"github.com/wago-org/wago"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func TestApplicationArtifactReloadPreservesNativeStackPolicy(t *testing.T) {
	for _, stackBytes := range []uint64{8 << 20, 1 << 20} {
		for _, reload := range []bool{false, true} {
			t.Run(fmt.Sprintf("stack=%d/reload=%t", stackBytes, reload), func(t *testing.T) {
				out := filepath.Join(t.TempDir(), "capture")
				var actual uint64
				h := Harness{
					ID: "stack-policy", Contract: "identity37", WorkUnit: "call", Wasm: fixtureModule(),
					RuntimeConfig: wago.NewRuntimeConfig().WithBoundsChecks(wago.BoundsChecksExplicit).WithNativeStackBytes(stackBytes),
					Execute: func(in *wago.Instance) error {
						// Inspect the mapped engine rather than inferring its capacity
						// from the requested configuration or manifest. This is test-only
						// access to existing runtime state, without a new public API.
						field := reflect.ValueOf(in).Elem().FieldByName("eng")
						engine := reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Interface().(*coreruntime.Engine)
						actual = engine.StackBytes()
						result, err := in.Invoke("identity", 37)
						if err != nil || len(result) != 1 || result[0] != 37 {
							return fmt.Errorf("identity(37): %v, %v", result, err)
						}
						return nil
					},
				}
				if err := RunWithHarness(Options{Out: out, Backend: "none", Phase: "execute", Iterations: 1, ReloadArtifact: reload, Bounds: "explicit", Rate: 99}, h); err != nil {
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
				if actual != stackBytes || manifest.Config["native_stack_bytes"] != float64(actual) || !manifest.Complete {
					t.Fatalf("stack requested=%d actual=%d reported=%v complete=%t", stackBytes, actual, manifest.Config["native_stack_bytes"], manifest.Complete)
				}
			})
		}
	}
}
