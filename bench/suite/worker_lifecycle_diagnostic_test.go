//go:build !windows

package wagobench

import (
	"fmt"
	"runtime"
	"sync"
	"testing"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/internal/functionworkers"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

type workerDiagnosticConfig struct {
	api                                                          string
	requested, passed, validationLimit, backendLimit, gomaxprocs int
}

func configureWorkerDiagnostic(api string, requested, functions, bodyBytes int) workerDiagnosticConfig {
	c := workerDiagnosticConfig{api: api, requested: requested, passed: requested, gomaxprocs: runtime.GOMAXPROCS(0)}
	if api == "policy" || api == "public" {
		c.passed = functionworkers.Resolve(requested, functions, bodyBytes)
	}
	c.validationLimit = max(1, min(c.passed, functions))
	c.backendLimit = shared.ResolveWorkers(c.passed, functions, c.gomaxprocs)
	return c
}

func (c workerDiagnosticConfig) run(stage string, mod *wasm.Module, data []byte, cfg *wago.RuntimeConfig) error {
	switch stage {
	case "validate":
		return wasm.ValidateModuleWithWorkers(mod, c.passed)
	case "backend":
		return compileWorkerDiagnosticBackend(mod, c.passed)
	default:
		compiled, err := wago.Compile(cfg, data)
		if err != nil {
			return err
		}
		return compiled.Close()
	}
}

func BenchmarkWorkerLifecycleDiagnostic(b *testing.B) {
	if !*lifecycleDiagnostics {
		b.Skip("enable with -wago.bench.lifecycle")
	}
	for _, m := range loadCorpus(b) {
		switch m.ID {
		case "tiny", "many_funcs", "json-as", "lua":
		default:
			continue
		}
		b.Run(m.ID, func(b *testing.B) {
			for _, callers := range []int{1, 4, runtime.GOMAXPROCS(0)} {
				for _, stage := range []string{"validate", "backend", "full"} {
					apis := []string{"raw", "policy"}
					if stage == "full" {
						apis = []string{"public"}
					}
					for _, api := range apis {
						for _, requested := range []int{0, 1, 2, 4, 8} {
							if api == "policy" && requested != 0 {
								continue
							}
							b.Run(fmt.Sprintf("callers=%d/%s/%s/requested=%d", callers, stage, api, requested), func(b *testing.B) {
								mod := m.decoded(b)
								if err := wasm.ValidateModule(mod); err != nil {
									b.Fatal(err)
								}
								bodyBytes, largest := 0, 0
								for _, f := range mod.Code {
									bodyBytes += len(f.BodyBytes)
									largest = max(largest, len(f.BodyBytes))
								}
								config := configureWorkerDiagnostic(api, requested, len(mod.Code), bodyBytes)
								cfg := wago.NewRuntimeConfig().WithFunctionWorkers(config.requested)
								modules := []*wasm.Module{mod}
								for i := 1; i < callers; i++ {
									private := m.decoded(b)
									if err := wasm.ValidateModule(private); err != nil {
										b.Fatal(err)
									}
									modules = append(modules, private)
								}
								b.ReportAllocs()
								b.ResetTimer()
								if callers == 1 {
									for i := 0; i < b.N; i++ {
										if err := config.run(stage, mod, m.bytes, cfg); err != nil {
											b.Fatal(err)
										}
									}
								} else {
									var wg sync.WaitGroup
									wg.Add(callers)
									for worker := 0; worker < callers; worker++ {
										go func(worker int) {
											defer wg.Done()
											for i := worker; i < b.N; i += callers {
												if err := config.run(stage, modules[worker], m.bytes, cfg); err != nil {
													b.Error(err)
													return
												}
											}
										}(worker)
									}
									wg.Wait()
								}
								b.StopTimer()
								b.ReportMetric(float64(config.requested), "requested-workers")
								b.ReportMetric(float64(config.passed), "phase-input-workers")
								if stage != "backend" {
									b.ReportMetric(float64(config.validationLimit), "validation-worker-limit")
								}
								if stage != "validate" {
									b.ReportMetric(float64(config.backendLimit), "backend-worker-limit")
								}
								b.ReportMetric(float64(config.gomaxprocs), "gomaxprocs")
								b.ReportMetric(float64(callers), "callers")
								b.ReportMetric(float64(len(mod.Code)), "functions")
								b.ReportMetric(float64(bodyBytes), "body-bytes")
								b.ReportMetric(float64(largest), "largest-body-bytes")
							})
						}
					}
				}
			}
		})
	}
}

func TestWorkerDiagnosticConfiguration(t *testing.T) {
	previous := runtime.GOMAXPROCS(4)
	defer runtime.GOMAXPROCS(previous)
	for _, tc := range []struct {
		api                                                          string
		requested, functions, bodyBytes, passed, validation, backend int
	}{
		{"raw", 0, 100, 1 << 20, 0, 1, 1},
		{"raw", 1, 100, 1 << 20, 1, 1, 1},
		{"raw", 8, 100, 1 << 20, 8, 8, 4},
		{"raw", 8, 2, 9, 8, 2, 2},
		{"policy", 0, 100, 1 << 20, 4, 4, 4},
		{"policy", 0, 2, 9, 1, 1, 1},
		{"policy", 0, 2, 1 << 20, 2, 2, 2},
		{"public", 0, 100, 1 << 20, 4, 4, 4},
		{"public", 1, 100, 1 << 20, 1, 1, 1},
		{"public", 8, 100, 1 << 20, 4, 4, 4},
		{"public", 8, 2, 9, 2, 2, 2},
		{"public", 0, 0, 0, 1, 1, 1},
	} {
		t.Run(fmt.Sprintf("%s/%d/%d/%d", tc.api, tc.requested, tc.functions, tc.bodyBytes), func(t *testing.T) {
			c := configureWorkerDiagnostic(tc.api, tc.requested, tc.functions, tc.bodyBytes)
			if c.passed != tc.passed || c.validationLimit != tc.validation || c.backendLimit != tc.backend || c.gomaxprocs != 4 {
				t.Fatalf("configuration %+v; want passed=%d validation=%d backend=%d", c, tc.passed, tc.validation, tc.backend)
			}
		})
	}
}
