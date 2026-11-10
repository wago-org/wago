//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func atomicWaitProbeModule(sub byte, zero bool, offset uint32) []byte {
	align := byte(2)
	if sub == 2 {
		align = 3
	}
	body := []byte{0x20, 0x00}
	if sub == 0 && zero {
		body = append(body, 0x41, 0x00)
	} else {
		body = append(body, 0x20, 0x01)
		if sub != 2 {
			body = append(body, 0xa7)
		}
	}
	if sub != 0 {
		if zero {
			body = append(body, 0x42, 0x00)
		} else {
			body = append(body, 0x20, 0x02)
		}
	}
	body = append(body, 0xfe, sub, align)
	body = append(body, wasmtest.ULEB(offset)...)
	body = append(body, 0x0b)
	memoryImport := append(wasmtest.Name("env"), wasmtest.Name("memory")...)
	memoryImport = append(memoryImport, 0x02, 0x03, 0x01, 0x01)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I64, wasm.I64}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(memoryImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

// The loop returns the sum of all atomic results, so the benchmark also checks
// the selected path. Inputs are iterations, address, expected/count, timeout.
func atomicWaitLoopModule(sub byte, constantTimeout bool) []byte {
	align := byte(2)
	if sub == 2 {
		align = 3
	}
	body := []byte{
		0x02, 0x40, 0x03, 0x40, // block; loop
		0x20, 0x04, 0x20, 0x00, 0x4f, 0x0d, 0x01, // counter >= iterations: break
		0x20, 0x05, // sum
		0x20, 0x01, // address
	}
	if sub == 0 && constantTimeout {
		body = append(body, 0x41, 0x00)
	} else {
		body = append(body, 0x20, 0x02)
		if sub != 2 {
			body = append(body, 0xa7) // i32.wrap_i64
		}
	}
	if sub != 0 {
		if constantTimeout {
			body = append(body, 0x42, 0x00)
		} else {
			body = append(body, 0x20, 0x03)
		}
	}
	body = append(body,
		0xfe, sub, align, 0x00,
		0x6a, 0x21, 0x05, // sum += atomic result
		0x20, 0x04, 0x41, 0x01, 0x6a, 0x21, 0x04, // counter++
		0x0c, 0x00, 0x0b, 0x0b, 0x20, 0x05, 0x0b,
	)
	entry := []byte{0x01, 0x02, 0x7f} // two i32 locals
	entry = append(entry, body...)
	entry = append(wasmtest.ULEB(uint32(len(entry))), entry...)
	memoryImport := append(wasmtest.Name("env"), wasmtest.Name("memory")...)
	memoryImport = append(memoryImport, 0x02, 0x03, 0x01, 0x01)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I64, wasm.I64}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(memoryImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(entry)),
	)
}

func BenchmarkAtomicWaitFastLoop(b *testing.B) {
	const iterations = 1024
	config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit)
	for _, tc := range []struct {
		name           string
		sub            byte
		constant       bool
		value, timeout uint64
		want           uint64
	}{
		{"wait32-mismatch", 1, false, 1, ^uint64(0), iterations},
		{"wait64-mismatch", 2, false, 1 << 40, ^uint64(0), iterations},
		{"wait32-timeout", 1, false, 0, 0, 2 * iterations},
		{"wait64-timeout", 2, false, 0, 0, 2 * iterations},
		{"wait32-constant-timeout", 1, true, 0, 0, 2 * iterations},
		{"wait64-constant-timeout", 2, true, 0, 0, 2 * iterations},
		{"notify-empty", 0, false, ^uint64(0), 0, 0},
		{"notify-zero", 0, false, 0, 0, 0},
		{"notify-constant-zero", 0, true, 0, 0, 0},
		{"notify-occupied-other-address", 0, false, 1, 0, 0},
	} {
		b.Run(tc.name, func(b *testing.B) {
			compiled, err := Compile(config, atomicWaitLoopModule(tc.sub, tc.constant))
			if err != nil {
				b.Fatal(err)
			}
			defer compiled.Close()
			memory, err := NewSharedMemory(1, 1)
			if err != nil {
				b.Fatal(err)
			}
			defer memory.Close()
			instance, err := Instantiate(compiled, testImports("env.memory", memory))
			if err != nil {
				b.Fatal(err)
			}
			defer instance.Close()
			if tc.name == "notify-occupied-other-address" {
				ctx, cancel := context.WithCancel(context.Background())
				done := make(chan struct{})
				go func() { defer close(done); _, _ = memory.wait32(ctx, 8, 0, -1) }()
				b.Cleanup(func() { cancel(); <-done })
				deadline := time.Now().Add(5 * time.Second)
				for {
					s := memory.state.Load()
					s.mu.Lock()
					ws := s.waiterStateLocked(false)
					active := ws != nil && ws.active == 1
					s.mu.Unlock()
					if active {
						break
					}
					if time.Now().After(deadline) {
						b.Fatal("waiter did not park")
					}
					runtime.Gosched()
				}
			}
			args := []uint64{iterations, 0, tc.value, tc.timeout}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				out, err := instance.Invoke("run", args...)
				if err != nil {
					b.Fatal(err)
				}
				if len(out) != 1 || out[0] != tc.want {
					b.Fatalf("result = %v, want %d", out, tc.want)
				}
			}
			b.ReportMetric(float64(len(compiled.code)), "code-B")
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/iterations, "ns/atomic")
		})
	}
}

func BenchmarkAtomicWaitFastInvoke(b *testing.B) {
	config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit)
	for _, tc := range []struct {
		name  string
		sub   byte
		zero  bool
		value uint64
	}{
		{"wait32-mismatch", 1, false, 1},
		{"wait64-mismatch", 2, false, 1 << 40},
		{"wait32-timeout", 1, false, 0},
		{"wait64-timeout", 2, false, 0},
		{"wait32-constant-timeout", 1, true, 0},
		{"wait64-constant-timeout", 2, true, 0},
		{"notify-empty", 0, false, 1},
		{"notify-constant-zero", 0, true, 0},
	} {
		b.Run(tc.name, func(b *testing.B) {
			c, err := Compile(config, atomicWaitProbeModule(tc.sub, tc.zero, 0))
			if err != nil {
				b.Fatal(err)
			}
			defer c.Close()
			m, err := NewSharedMemory(1, 1)
			if err != nil {
				b.Fatal(err)
			}
			defer m.Close()
			in, err := Instantiate(c, testImports("env.memory", m))
			if err != nil {
				b.Fatal(err)
			}
			defer in.Close()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := in.Invoke("run", 0, tc.value, 0); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(len(c.code)), "code-B")
		})
	}
}

func BenchmarkAtomicWaitFastCompile(b *testing.B) {
	config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit)
	for _, tc := range []struct {
		name string
		sub  byte
		zero bool
	}{
		{"wait32-dynamic", 1, false}, {"wait32-zero", 1, true},
		{"wait64-dynamic", 2, false}, {"wait64-zero", 2, true},
		{"notify-dynamic", 0, false}, {"notify-zero", 0, true},
	} {
		b.Run(tc.name, func(b *testing.B) {
			module := atomicWaitLoopModule(tc.sub, tc.zero)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				c, err := Compile(config, module)
				if err != nil {
					b.Fatal(err)
				}
				if err := c.Close(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkAtomicNotifyFastParallel(b *testing.B) {
	const iterations = 1024
	config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV2 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit)
	c, err := Compile(config, atomicWaitLoopModule(0, false))
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	m, err := NewSharedMemory(1, 1)
	if err != nil {
		b.Fatal(err)
	}
	defer m.Close()
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		in, err := Instantiate(c, testImports("env.memory", m))
		if err != nil {
			b.Error(err)
			return
		}
		defer in.Close()
		for pb.Next() {
			if _, err := in.Invoke("run", iterations, 0, 1, 0); err != nil {
				b.Error(err)
				return
			}
		}
	})
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/iterations, "ns/atomic")
}
