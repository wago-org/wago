//go:build ((linux && (amd64 || arm64)) || (darwin && arm64)) && !tinygo

package wago

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func sharedGrowHighDeltaModule(index byte, imported bool) []byte {
	selected := []byte{7, 1, 2}
	if imported {
		selected = []byte{7, 1, 0x81, 0x80, 0x80, 0x80, 0x80, 0x20}
	} // maximum2^40+1
	memories := [][]byte{selected, {1, 0, 1}}
	if index != 0 {
		memories[0], memories[1] = memories[1], memories[0]
	}
	// Select between two runtime deltas inside the guest loop.
	loop := []byte{2, 1, 0x7f, 1, 0x7e, 0x02, 0x40, 0x03, 0x40, 0x20, 3, 0x20, 0, 0x4f, 0x0d, 1,
		0x20, 4, 0x20, 3, 0x41, 1, 0x71, 0x04, 0x7e, 0x20, 1, 0x05, 0x20, 2, 0x0b, 0x40, index,
		0x7c, 0x21, 4, 0x20, 3, 0x41, 1, 0x6a, 0x21, 3, 0x0c, 0, 0x0b, 0x0b, 0x20, 4, 0x0b}
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(
		wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}),
		wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I64, wasm.I64}, []wasm.ValType{wasm.I64}),
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I64})))}
	if imported {
		var imports [][]byte
		for i, memory := range memories {
			entry := append(wasmtest.Name("env"), wasmtest.Name(fmt.Sprintf("memory%d", i))...)
			imports = append(imports, append(append(entry, 2), memory...))
		}
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(imports...)))
	}
	sections = append(sections, wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{1}, []byte{2})))
	if !imported {
		sections = append(sections, wasmtest.Section(5, wasmtest.Vec(memories...)))
	}
	sections = append(sections, wasmtest.Section(7, wasmtest.Vec(
		wasmtest.ExportEntry("run", 0, 0), wasmtest.ExportEntry("loop", 0, 1), wasmtest.ExportEntry("size", 0, 2),
		wasmtest.ExportEntry("memory0", 2, 0), wasmtest.ExportEntry("memory1", 2, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x40, index, 0x0b}),
			append(wasmtest.ULEB(uint32(len(loop))), loop...), wasmtest.Code([]byte{0x3f, index, 0x0b}))))
	return wasmtest.Module(sections...)
}

func sharedGrowHighDeltaConfig() *RuntimeConfig {
	return NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit)
}

func sharedGrowHighDeltaInstance(t *testing.T, index byte, imported, closeProducer bool) *Instance {
	t.Helper()
	cfg := sharedGrowHighDeltaConfig()
	providerCode, err := cfg.Compile(sharedGrowHighDeltaModule(index, false))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { providerCode.Close() })
	provider, err := Instantiate(providerCode)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { provider.Close() })
	if !imported {
		return provider
	}
	imports := NewImports()
	for i := 0; i < 2; i++ {
		name := fmt.Sprintf("memory%d", i)
		memory, err := provider.ExportedMemory(name)
		if err != nil {
			t.Fatal(err)
		}
		imports.Memory("env", name, memory)
	}
	c, err := cfg.Compile(sharedGrowHighDeltaModule(index, true))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	in, err := Instantiate(c, InstantiateOptions{Imports: imports})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { in.Close() })
	if closeProducer {
		provider.Close()
	}
	return in
}

func TestSharedGrowHighDeltaResults(t *testing.T) {
	for _, index := range []byte{0, 1} {
		for _, shape := range []struct{ imported, closed bool }{{}, {true, false}, {true, true}} {
			t.Run(fmt.Sprintf("index%d/import%t/closed%t", index, shape.imported, shape.closed), func(t *testing.T) {
				in := sharedGrowHighDeltaInstance(t, index, shape.imported, shape.closed)
				memory := in.memoryDir.memories[index]
				data := memory.hostBytes()
				data[0], data[len(data)-1] = 0x71, 0x83
				snapshot := append([]byte(nil), data...)
				for _, delta := range []uint64{0xffffffff, 1 << 32, (1 << 32) + 1, 1 << 39, ^uint64(0)} {
					got, err := in.Invoke("run", delta)
					if err != nil || len(got) != 1 || got[0] != ^uint64(0) {
						t.Fatalf("grow(%x):%x,%v", delta, got, err)
					}
					pages, ok := memory.currentPages()
					if !ok || pages != 1 || !bytes.Equal(snapshot, memory.hostBytes()) {
						t.Fatalf("grow(%x) changed backing:pages%d/open%t", delta, pages, ok)
					}
				}
				for _, step := range []struct{ delta, want uint64 }{{0, 1}, {1, 1}, {0, 2}, {1, ^uint64(0)}} {
					got, err := in.Invoke("run", step.delta)
					if err != nil || len(got) != 1 || got[0] != step.want {
						t.Fatalf("control grow(%x):%x,%v,want%x", step.delta, got, err, step.want)
					}
				}
				if pages, ok := memory.currentPages(); !ok || pages != 2 {
					t.Fatalf("control size:%d,%t", pages, ok)
				}
				if !bytes.Equal(snapshot, memory.hostBytes()[:len(snapshot)]) {
					t.Fatal("growth changed prior contents")
				}
			})
		}
	}
}

func TestSharedGrowHighDeltaSelectedRead(t *testing.T) {
	for _, index := range []byte{0, 1} {
		for _, shape := range []struct{ imported, closed bool }{{}, {true, false}, {true, true}} {
			for _, delta := range []uint64{0, 0xffffffff, 1 << 32, (1 << 32) + 1, 1 << 39, ^uint64(0)} {
				t.Run(fmt.Sprintf("index%d/import%t/closed%t/delta%x", index, shape.imported, shape.closed, delta), func(t *testing.T) {
					in := sharedGrowHighDeltaInstance(t, index, shape.imported, shape.closed)
					memory := in.memoryDir.memories[index]
					state := memory.state.Load()
					if state == nil {
						t.Fatal("missing shared memory state")
					}
					state.mu.Lock()
					locked := true
					defer func() {
						if locked {
							state.mu.Unlock()
						}
					}()
					type outcome struct {
						result    uint64
						recovered any
					}
					started, done := make(chan struct{}), make(chan outcome, 1)
					go func() {
						close(started)
						result := outcome{}
						func() {
							defer func() { result.recovered = recover() }()
							var values [1]uint64
							in.dispatchAtomicWaitHelper(shared.AtomicHelperMemoryGrow, []uint64{delta, uint64(index)}, values[:])
							result.result = values[0]
						}()
						done <- result
					}()
					<-started
					select {
					case result := <-done:
						state.mu.Unlock()
						locked = false
						t.Fatalf("grow returned before the selected memory read guard was released:result%x panic%v", result.result, result.recovered)
					case <-time.After(25 * time.Millisecond):
					}
					state.mu.Unlock()
					locked = false
					select {
					case result := <-done:
						want := ^uint64(0)
						if delta == 0 {
							want = 1
						}
						if result.recovered != nil || result.result != want {
							t.Fatalf("after release:result%x panic%v,want%x", result.result, result.recovered, want)
						}
					case <-time.After(5 * time.Second):
						t.Fatal("grow did not complete after the selected memory read guard was released")
					}
				})
			}
		}
	}
}

func BenchmarkSharedGrowHighDelta(b *testing.B) {
	const iterations = 1024
	for _, index := range []byte{0, 1} {
		b.Run(fmt.Sprintf("index%d", index), func(b *testing.B) {
			c, err := sharedGrowHighDeltaConfig().Compile(sharedGrowHighDeltaModule(index, false))
			if err != nil {
				b.Fatal(err)
			}
			defer c.Close()
			in, err := Instantiate(c)
			if err != nil {
				b.Fatal(err)
			}
			defer in.Close()
			for _, path := range []struct {
				name          string
				first, second uint64
			}{{"high", (1 << 32) + 1, (1 << 32) + 1}, {"normal-failure", 0xffffffff, 0xffffffff}, {"mixed", (1 << 32) + 1, 0xffffffff}} {
				b.Run(path.name, func(b *testing.B) {
					b.ReportAllocs()
					for range b.N {
						got, err := in.Invoke("loop", iterations, path.first, path.second)
						if err != nil || len(got) != 1 || got[0] != ^uint64(0)-iterations+1 {
							b.Fatalf("loop:%x,%v", got, err)
						}
					}
					b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/iterations, "ns/grow")
					b.ReportMetric(float64(c.CodeSize()), "native-B")
				})
			}
		})
	}
}

func BenchmarkSharedGrowHighDeltaCompile(b *testing.B) {
	for _, index := range []byte{0, 1} {
		b.Run(fmt.Sprintf("index%d", index), func(b *testing.B) {
			cfg, module := sharedGrowHighDeltaConfig(), sharedGrowHighDeltaModule(index, false)
			b.ReportAllocs()
			for range b.N {
				c, err := cfg.Compile(module)
				if err != nil {
					b.Fatal(err)
				}
				b.ReportMetric(float64(c.CodeSize()), "native-B")
				c.Close()
			}
		})
	}
}
