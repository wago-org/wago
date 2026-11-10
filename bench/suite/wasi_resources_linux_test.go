//go:build linux && amd64 && wago_guardpage

package wagobench

import (
	"os"
	"runtime/debug"
	"testing"

	"github.com/wago-org/wago"
)

func TestWASIResources(t *testing.T) {
	if !*lifecycleDiagnostics {
		t.Skip("enable with -wago.bench.lifecycle")
	}
	modules := []corpusModule{minimalWASICommand()}
	for _, m := range commandCorpus(t) {
		if m.ID == "cjson" || m.ID == "tinyxml2" {
			modules = append(modules, m)
		}
	}
	for _, m := range modules {
		t.Run(m.ID, func(t *testing.T) {
			c, err := wago.Compile(nil, m.bytes)
			if err != nil {
				t.Fatal(err)
			}
			stdin := commandInput(t, m)
			if _, err := runWagoCommand(m, c, stdin, false); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadDir("/proc/self/fd")
			if err != nil {
				t.Fatal(err)
			}
			lifecycleResourceSnapshot(t, m.ID, "warm-normal")
			for i := 0; i < 1000; i++ {
				if _, err := runWagoCommand(m, c, stdin, false); err != nil {
					t.Fatal(err)
				}
			}
			lifecycleResourceSnapshot(t, m.ID, "closed-normal")
			if err := c.Close(); err != nil {
				t.Fatal(err)
			}
			lifecycleResourceSnapshot(t, m.ID, "released-normal")
			after, err := os.ReadDir("/proc/self/fd")
			if err != nil {
				t.Fatal(err)
			}
			if len(before) != len(after) {
				t.Fatalf("descriptors grew: %d -> %d", len(before), len(after))
			}
			debug.FreeOSMemory()
			lifecycleResourceSnapshot(t, m.ID, "released-gc")
		})
	}
}
