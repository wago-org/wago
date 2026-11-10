//go:build linux && amd64 && wago_guardpage

package wagobench

import (
	"context"
	"fmt"
	"runtime/debug"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/wago-org/wago"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func memoryPolicyModule(pages, dataBytes int) []byte {
	sections := [][]byte{
		wasmtest.Section(5, wasmtest.Vec(append(append([]byte{1}, wasmtest.ULEB(uint32(pages))...), wasmtest.ULEB(32)...))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("memory", 2, 0))),
	}
	if dataBytes != 0 {
		data := make([]byte, dataBytes)
		for i := range data {
			data[i] = 0xa5
		}
		segment := append([]byte{0, 0x41, 0, 0x0b}, wasmtest.ULEB(uint32(dataBytes))...)
		segment = append(segment, data...)
		sections = append(sections, wasmtest.Section(11, wasmtest.Vec(segment)))
	}
	return wasmtest.Module(sections...)
}

func TestMemoryPolicyReleasedResources(t *testing.T) {
	if !*lifecycleDiagnostics {
		t.Skip("enable with -wago.bench.lifecycle")
	}
	for _, pages := range []int{6, 16} {
		for _, dataBytes := range []int{0, 4096, 65536, pages * 65536} {
			for _, engine := range []string{"wago-guarded", "wazero-default", "wazero-max-capacity"} {
				t.Run(fmt.Sprintf("pages=%d/data=%d/%s", pages, dataBytes, engine), func(t *testing.T) {
					data := memoryPolicyModule(pages, dataBytes)
					run, closeCompiled := prepareMemoryPolicy(t, engine, data)
					defer func() {
						if closeCompiled != nil {
							_ = closeCompiled()
						}
					}()
					lifecycleResourceSnapshot(t, engine, "compiled-normal")
					if err := run(); err != nil {
						t.Fatal(err)
					}
					lifecycleResourceSnapshot(t, engine, "first-closed-normal")
					for i := 0; i < 128; i++ {
						if err := run(); err != nil {
							t.Fatal(err)
						}
					}
					lifecycleResourceSnapshot(t, engine, "warm-closed-normal")
					if err := closeCompiled(); err != nil {
						t.Fatal(err)
					}
					run = nil
					closeCompiled = nil
					lifecycleResourceSnapshot(t, engine, "released-normal")
					debug.FreeOSMemory()
					lifecycleResourceSnapshot(t, engine, "released-gc")
				})
			}
		}
	}
}

func prepareMemoryPolicy(tb testing.TB, engine string, data []byte) (func() error, func() error) {
	tb.Helper()
	var run func() error
	var closeCompiled func() error
	if engine == "wago-guarded" {
		compiled, err := wago.Compile(nil, data)
		if err != nil {
			tb.Fatal(err)
		}
		closeCompiled = compiled.Close
		run = func() error {
			in, err := wago.Instantiate(compiled)
			if err != nil {
				return err
			}
			return in.Close()
		}
	} else {
		ctx := context.Background()
		cfg := wazero.NewRuntimeConfigCompiler().WithMemoryLimitPages(32)
		if engine == "wazero-max-capacity" {
			cfg = cfg.WithMemoryCapacityFromMax(true)
		}
		rt := wazero.NewRuntimeWithConfig(ctx, cfg)
		compiled, err := rt.CompileModule(ctx, data)
		if err != nil {
			rt.Close(ctx)
			tb.Fatal(err)
		}
		closeCompiled = func() error { return rt.Close(ctx) }
		run = func() error {
			in, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("").WithStartFunctions())
			if err != nil {
				return err
			}
			return in.Close(ctx)
		}
	}
	return run, closeCompiled
}

func BenchmarkMemoryPolicyLifecycleDiagnostic(b *testing.B) {
	if !*lifecycleDiagnostics {
		b.Skip("enable with -wago.bench.lifecycle")
	}
	for _, pages := range []int{6, 16} {
		for _, size := range []int{0, 4096, 65536, pages * 65536} {
			for _, engine := range []string{"wago-guarded", "wazero-default", "wazero-max-capacity"} {
				b.Run(fmt.Sprintf("pages=%d/data=%d/%s", pages, size, engine), func(b *testing.B) {
					run, closeCompiled := prepareMemoryPolicy(b, engine, memoryPolicyModule(pages, size))
					defer closeCompiled()
					if err := run(); err != nil {
						b.Fatal(err)
					}
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						if err := run(); err != nil {
							b.Fatal(err)
						}
					}
					b.StopTimer()
				})
			}
		}
	}
}
