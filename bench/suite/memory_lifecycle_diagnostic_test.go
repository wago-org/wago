//go:build linux && amd64 && wago_guardpage

package wagobench

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"syscall"
	"testing"

	"github.com/wago-org/wago"
	core "github.com/wago-org/wago/src/core/runtime"
)

func lifecycleMemoryModules(tb testing.TB) []corpusModule {
	var out []corpusModule
	for _, m := range loadCorpus(tb) {
		switch m.ID {
		case "tiny", "utf8proc", "pcre2", "xxhash":
			out = append(out, m)
		}
	}
	return out
}

func TestLifecycleFixtureMetadata(t *testing.T) {
	if !*lifecycleDiagnostics {
		t.Skip("enable with -wago.bench.lifecycle")
	}
	for _, m := range lifecycleMemoryModules(t) {
		c, err := wago.Compile(nil, m.bytes)
		if err != nil {
			t.Fatal(err)
		}
		mod := m.decoded(t)
		dataBytes := 0
		for _, d := range c.Data {
			dataBytes += len(d.Bytes)
		}
		value := map[string]any{"id": m.ID, "sha256": m.ArtifactSHA256, "initial_pages": c.MemMinPages, "maximum_pages": c.MemMaxPages, "declared_maximum": c.MemHasMax, "active_segments": len(c.Data), "active_bytes": dataBytes, "tables": mod.Tables, "imports": len(mod.Imports), "globals": len(c.Globals), "start": c.HasStart, "bounds": "signals", "allocation": "guarded, one cached reservation"}
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		t.Log(string(data))
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func BenchmarkInstanceLifecycleDiagnostic(b *testing.B) {
	if !*lifecycleDiagnostics {
		b.Skip("enable with -wago.bench.lifecycle")
	}
	for _, m := range lifecycleMemoryModules(b) {
		b.Run(m.ID, func(b *testing.B) {
			c, err := wago.Compile(nil, m.bytes)
			if err != nil {
				b.Fatal(err)
			}
			defer c.Close()
			imports := hostStubs(c)
			probe, err := wago.Instantiate(c, wago.InstantiateOptions{Imports: imports})
			if err != nil {
				b.Fatal(err)
			}
			if err := probe.Close(); err != nil {
				b.Fatal(err)
			}
			for _, phase := range []string{"FirstCompiled", "Instantiate", "Close", "Lifecycle"} {
				b.Run(phase, func(b *testing.B) {
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						current := c
						if phase == "FirstCompiled" {
							b.StopTimer()
							current, err = wago.Compile(nil, m.bytes)
							if err != nil {
								b.Fatal(err)
							}
							b.StartTimer()
						}
						if phase == "Close" {
							b.StopTimer()
						}
						in, err := wago.Instantiate(current, wago.InstantiateOptions{Imports: imports})
						if err != nil {
							b.Fatal(err)
						}
						if phase == "Instantiate" {
							b.StopTimer()
						}
						if phase == "Close" {
							b.StartTimer()
						}
						if err := in.Close(); err != nil {
							b.Fatal(err)
						}
						if phase == "Instantiate" {
							b.StartTimer()
						}
						if phase == "FirstCompiled" {
							b.StopTimer()
							if err := current.Close(); err != nil {
								b.Fatal(err)
							}
							b.StartTimer()
						}
					}
				})
			}
		})
	}
}

// Size and data bytes vary independently. Each operation acquires memory,
// copies active bytes, and releases it. Neither zeroing nor reclamation is
// moved outside the timer. Fresh bypasses the existing one-slot cache.
func BenchmarkMemoryReuseDiagnostic(b *testing.B) {
	if !*lifecycleDiagnostics {
		b.Skip("enable with -wago.bench.lifecycle")
	}
	for _, pages := range []int{1, 5, 6, 7, 16} {
		for _, dataBytes := range []int{0, 4096, 65536} {
			for _, mode := range []string{"guard-reuse", "guard-fresh", "explicit-reuse"} {
				b.Run(fmt.Sprintf("pages=%d/data=%d/%s", pages, dataBytes, mode), func(b *testing.B) {
					data := make([]byte, dataBytes)
					for i := range data {
						data[i] = 0xab
					}
					b.ReportAllocs()
					var before, after syscall.Rusage
					if err := syscall.Getrusage(syscall.RUSAGE_SELF, &before); err != nil {
						b.Fatal(err)
					}
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						var jm *core.JobMemory
						var err error
						switch mode {
						case "guard-reuse":
							jm, err = core.AcquireJobMemoryGuarded(pages*65536, 32*65536)
						case "guard-fresh":
							jm, err = core.NewJobMemoryGuarded(pages*65536, 32*65536)
						case "explicit-reuse":
							jm, err = core.AcquireJobMemoryGrowable(pages*65536, 32*65536)
						}
						if err != nil {
							b.Fatal(err)
						}
						copy(jm.LinearMemory(), data)
						if mode == "guard-fresh" {
							err = jm.Close()
						} else {
							err = core.ReleaseJobMemory(jm)
						}
						if err != nil {
							b.Fatal(err)
						}
					}
					b.StopTimer()
					if err := syscall.Getrusage(syscall.RUSAGE_SELF, &after); err != nil {
						b.Fatal(err)
					}
					b.ReportMetric(float64(after.Minflt-before.Minflt)/float64(b.N), "minor-faults/op")
				})
			}
		}
	}
}

func BenchmarkMemoryGrowReuseDiagnostic(b *testing.B) {
	if !*lifecycleDiagnostics {
		b.Skip("enable with -wago.bench.lifecycle")
	}
	c, err := wago.Compile(nil, memoryGrowOnceModule(4))
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		in, err := wago.Instantiate(c)
		if err != nil {
			b.Fatal(err)
		}
		result, err := in.Invoke("grow", 2)
		if err != nil || len(result) != 1 || result[0] != 1 {
			in.Close()
			b.Fatalf("grow: %v, %v", result, err)
		}
		mem := in.Memory().UnsafeBytes()
		if len(mem) != 3*65536 || mem[0] != 0 || mem[len(mem)-1] != 0 {
			in.Close()
			b.Fatal("reused memory was not zero")
		}
		mem[0], mem[len(mem)-1] = 0xab, 0xcd
		if err := in.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func lifecycleResourceSnapshot(t *testing.T, id, phase string) {
	t.Helper()
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		t.Fatal(err)
	}
	status, err := os.ReadFile("/proc/self/smaps_rollup")
	if err != nil {
		t.Fatal(err)
	}
	var heap runtime.MemStats
	runtime.ReadMemStats(&heap)
	data, err := json.Marshal(map[string]any{"id": id, "phase": phase, "max_rss_kib": usage.Maxrss, "minor_faults": usage.Minflt, "major_faults": usage.Majflt, "native_mappings": core.ProcessNativeMemoryStats(), "heap_alloc": heap.HeapAlloc, "heap_inuse": heap.HeapInuse, "smaps": string(status)})
	if err != nil {
		t.Fatal(err)
	}
	t.Log(string(data))
}

func TestLifecycleResources(t *testing.T) {
	if !*lifecycleDiagnostics {
		t.Skip("enable with -wago.bench.lifecycle")
	}
	for _, m := range lifecycleMemoryModules(t) {
		c, err := wago.Compile(nil, m.bytes)
		if err != nil {
			t.Fatal(err)
		}
		imports := hostStubs(c)
		lifecycleResourceSnapshot(t, m.ID, "compiled")
		for i := 0; i < 1000; i++ {
			in, err := wago.Instantiate(c, wago.InstantiateOptions{Imports: imports})
			if err != nil {
				t.Fatal(err)
			}
			if i == 999 {
				lifecycleResourceSnapshot(t, m.ID, "live")
			}
			if err := in.Close(); err != nil {
				t.Fatal(err)
			}
		}
		lifecycleResourceSnapshot(t, m.ID, "closed")
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
		debug.FreeOSMemory()
		lifecycleResourceSnapshot(t, m.ID, "released-gc")
	}
}

func TestCommandLifecycleResources(t *testing.T) {
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
		c, err := wago.Compile(nil, m.bytes)
		if err != nil {
			t.Fatal(err)
		}
		stdin := commandInput(t, m)
		if _, err := runWagoCommand(m, c, stdin, false); err != nil {
			t.Fatal(err)
		}
		debug.FreeOSMemory()
		lifecycleResourceSnapshot(t, m.ID, "warm")
		for i := 0; i < 1000; i++ {
			if _, err := runWagoCommand(m, c, stdin, false); err != nil {
				t.Fatal(err)
			}
		}
		lifecycleResourceSnapshot(t, m.ID, "closed")
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
		debug.FreeOSMemory()
		lifecycleResourceSnapshot(t, m.ID, "released-gc")
	}
}

func BenchmarkAcquisitionLifecycleDiagnostic(b *testing.B) {
	if !*lifecycleDiagnostics {
		b.Skip("enable with -wago.bench.lifecycle")
	}
	b.Run("engine", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			engine, err := core.AcquireEngineWithStackBytes(core.DefaultNativeStackBytes)
			if err != nil {
				b.Fatal(err)
			}
			core.ReleaseEngine(engine)
		}
	})
	for _, size := range []int{4096, 65536} {
		b.Run(fmt.Sprintf("arena=%d", size), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				arena, err := core.AcquireArena(size)
				if err != nil {
					b.Fatal(err)
				}
				core.ReleaseArena(arena)
			}
		})
	}
}
