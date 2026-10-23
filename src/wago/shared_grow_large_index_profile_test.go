//go:build wago_profile && (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"runtime"
	"testing"
)

func TestSharedGrowLargeMemoryIndexHelperEntries(t *testing.T) {
	for _, index := range []uint32{1365, 1366, 4095} {
		for _, wide := range []bool{false, true} {
			t.Run(fmt.Sprintf("index%d/wide%t", index, wide), func(t *testing.T) {
				trace := NewCodeProfile(CodeProfileOptions{TraceBoundaries: true})
				t.Cleanup(func() { trace.Close() })
				cfg := sharedGrowLargeIndexConfig(t, wide)
				c, err := cfg.WithCodeProfile(trace).Compile(sharedGrowLargeIndexModule(wide, index, true))
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { c.Close() })
				in := sharedGrowLargeIndexInstance(t, cfg, c, wide)
				count := func() int {
					n := 0
					for _, span := range trace.Spans() {
						if span.Kind == "atomic-wait-helper" && span.Function == int(shared.AtomicHelperMemoryGrow) {
							n++
						}
					}
					return n
				}
				deltas := []uint64{0, 1}
				if sharedGrowLargeIndexPath == "constant" {
					deltas = []uint64{1}
				}
				for _, delta := range deltas {
					before := count()
					sharedGrowLargeIndexInvoke(t, in, wide, delta)
					want := 0
					if runtime.GOARCH == "arm64" && index > 1365 || sharedGrowLargeIndexPath == "zero" && delta != 0 || sharedGrowLargeIndexPath == "dynamic" && delta == 0 {
						want = 1
					}
					if got := count() - before; got != want {
						t.Fatalf("delta%d helper entries%d,want%d", delta, got, want)
					}
					t.Logf("delta%d helper entries%d", delta, count()-before)
				}
			})
		}
	}
}
