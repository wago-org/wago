//go:build linux && amd64 && wago_guardpage

package wagobench

import (
	"fmt"
	"runtime"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/wago-org/wago"
)

func TestWorkerResources(t *testing.T) {
	if !*lifecycleDiagnostics {
		t.Skip("enable with -wago.bench.lifecycle")
	}
	for _, m := range loadCorpus(t) {
		switch m.ID {
		case "tiny", "many_funcs", "json-as", "lua":
		default:
			continue
		}
		for _, callers := range []int{1, 4, 16} {
			for _, requested := range []int{1, 2, 4, 0} {
				t.Run(fmt.Sprintf("%s/callers=%d/requested=%d", m.ID, callers, requested), func(t *testing.T) {
					cfg := wago.NewRuntimeConfig().WithFunctionWorkers(requested)
					warm, err := wago.Compile(cfg, m.bytes)
					if err != nil {
						t.Fatal(err)
					}
					if err := warm.Close(); err != nil {
						t.Fatal(err)
					}
					mod := m.decoded(t)
					body := 0
					for _, f := range mod.Code {
						body += len(f.BodyBytes)
					}
					c := configureWorkerDiagnostic("public", requested, len(mod.Code), body)
					times := make([]time.Duration, callers*8)
					lifecycleResourceSnapshot(t, m.ID, "warm-normal")
					start := time.Now()
					var wg sync.WaitGroup
					wg.Add(callers)
					for caller := 0; caller < callers; caller++ {
						go func(caller int) {
							defer wg.Done()
							for i := 0; i < 8; i++ {
								begin := time.Now()
								compiled, err := wago.Compile(cfg, m.bytes)
								if err != nil {
									t.Error(err)
									return
								}
								if err := compiled.Close(); err != nil {
									t.Error(err)
									return
								}
								times[caller*8+i] = time.Since(begin)
							}
						}(caller)
					}
					wg.Wait()
					elapsed := time.Since(start)
					lifecycleResourceSnapshot(t, m.ID, "closed-normal")
					sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
					t.Logf("api=public requested=%d passed=%d validation_limit=%d backend_limit=%d GOMAXPROCS=%d callers=%d calls=%d aggregate_ns_per_call=%d individual_p50_ns=%d individual_max_ns=%d", requested, c.passed, c.validationLimit, c.backendLimit, runtime.GOMAXPROCS(0), callers, len(times), elapsed.Nanoseconds()/int64(len(times)), times[len(times)/2].Nanoseconds(), times[len(times)-1].Nanoseconds())
				})
			}
		}
	}
}
