//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func threadsGrowModule(imported bool, minimum, maximum uint32) []byte {
	limits := append([]byte{3}, wasmtest.ULEB(minimum)...)
	limits = append(limits, wasmtest.ULEB(maximum)...)
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(
		wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32, wasm.I32}),
		wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}),
	))}
	if imported {
		entry := append(wasmtest.Name("env"), wasmtest.Name("memory")...)
		entry = append(entry, 2)
		entry = append(entry, limits...)
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(entry)))
	}
	sections = append(sections, wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{1}, []byte{1}, []byte{2}, []byte{3})))
	if !imported {
		sections = append(sections, wasmtest.Section(5, wasmtest.Vec(limits)))
	}
	sections = append(sections,
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("grow", 0, 0), wasmtest.ExportEntry("size", 0, 1),
			wasmtest.ExportEntry("grow0", 0, 2), wasmtest.ExportEntry("touch", 0, 3),
			wasmtest.ExportEntry("observe", 0, 4), wasmtest.ExportEntry("memory", 2, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0x40, 0, 0x0b}),
			wasmtest.Code([]byte{0x3f, 0, 0x0b}),
			wasmtest.Code([]byte{0x41, 0, 0x40, 0, 0x0b}),
			// Return the previous size, fill newly grown memory, copy within it,
			// then load the copied byte without leaving this activation.
			wasmtest.Code([]byte{0x20, 0, 0x40, 0,
				0x20, 1, 0x41, 0x7f, 0x41, 33, 0xfc, 11, 0,
				0x20, 1, 0x41, 1, 0x6a, 0x20, 1, 0x41, 32, 0xfc, 10, 0, 0,
				0x20, 1, 0x2d, 0, 1, 0x0b}),
			// Signal entry, then observe growth in one bounded activation. The
			// countdown prevents a bad size load from hanging the test process.
			wasmtest.Code([]byte{0x41, 0, 0x41, 1, 0xfe, 0x17, 2, 0,
				0x02, 0x40, 0x03, 0x40,
				0x3f, 0, 0x20, 0, 0x4f, 0x0d, 1,
				0x20, 1, 0x45, 0x0d, 1,
				0x20, 1, 0x41, 1, 0x6b, 0x21, 1, 0x0c, 0,
				0x0b, 0x0b, 0x3f, 0, 0x0b}),
		)),
	)
	return wasmtest.Module(sections...)
}

func TestThreadsSharedMemoryGrowExecutionAndCodec(t *testing.T) {
	for _, imported := range []bool{false, true} {
		for _, minimum := range []uint32{0, 1} {
			for _, quota := range []uint32{0, 2} {
				t.Run(fmt.Sprintf("imported=%t/min=%d/quota=%d", imported, minimum, quota), func(t *testing.T) {
					cfg := NewRuntimeConfig().WithFeature(CoreFeatureThreads, true).WithBoundsChecks(BoundsChecksExplicit).WithMemoryLimitPages(quota)
					c, err := cfg.Compile(threadsGrowModule(imported, minimum, 3))
					if err != nil {
						t.Fatal(err)
					}
					defer c.Close()
					loaded := publicArtifactRoundTrip(t, c)
					for _, compiled := range []*Compiled{c, loaded} {
						opts := InstantiateOptions{memoryLimitPages: quota}
						if imported {
							memory, err := NewSharedMemory(minimum, 3)
							if err != nil {
								t.Fatal(err)
							}
							defer memory.Close()
							opts.Imports = testImports("env.memory", memory)
						}
						in, err := Instantiate(compiled, opts)
						if err != nil {
							t.Fatal(err)
						}
						defer in.Close()
						pages := minimum
						for _, delta := range []uint64{0, 1, 0, 0xffffffff, 1 << 32, 1, 1, 1} {
							want := uint64(0xffffffff)
							maximum := uint32(3)
							if quota != 0 {
								maximum = quota
							}
							if uint32(delta) <= maximum-pages {
								want = uint64(pages)
								pages += uint32(delta)
							}
							got, err := in.Invoke("grow", delta)
							if err != nil || len(got) != 1 || got[0] != want {
								t.Fatalf("grow %x = %v, %v; want %d", delta, got, err, want)
							}
							for _, name := range []string{"size", "grow0"} {
								got, err := in.Invoke(name)
								if err != nil || len(got) != 1 || got[0] != uint64(pages) {
									t.Fatalf("%s = %v, %v; want %d", name, got, err, pages)
								}
							}
							mem := in.mem()
							if len(mem) != int(pages)*65536 {
								t.Fatalf("host length = %d, want %d pages", len(mem), pages)
							}
							for _, b := range mem {
								if b != 0 {
									t.Fatal("grown memory was not zero initialized")
								}
							}
						}
					}
				})
			}
		}
	}
}

func TestThreadsSharedGrowMinimalFrame(t *testing.T) {
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(5, wasmtest.Vec([]byte{3, 0, 1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("grow", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x40, 0, 0x0b}))),
	)
	cfg := NewRuntimeConfig().WithFeature(CoreFeatureThreads, true).WithBoundsChecks(BoundsChecksExplicit)
	c, err := cfg.Compile(module)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if got, err := in.Invoke("grow", 0); err != nil || !slices.Equal(got, []uint64{0}) {
		t.Fatalf("grow 0 = %v, %v", got, err)
	}
}

func TestThreadsSharedMemoryGrowToMemory32Limit(t *testing.T) {
	cfg := NewRuntimeConfig().WithFeature(CoreFeatureThreads, true).WithBoundsChecks(BoundsChecksExplicit).WithMemoryLimitPages(65536)
	c, err := cfg.Compile(threadsGrowModule(false, 0, 65536))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, compiled := range []*Compiled{c, publicArtifactRoundTrip(t, c)} {
		in, err := Instantiate(compiled, InstantiateOptions{memoryLimitPages: 65536})
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		if got, err := in.Invoke("grow", 65536); err != nil || !slices.Equal(got, []uint64{0}) {
			t.Fatalf("grow to 4 GiB = %v, %v", got, err)
		}
		for _, name := range []string{"size", "grow0"} {
			if got, err := in.Invoke(name); err != nil || !slices.Equal(got, []uint64{65536}) {
				t.Fatalf("%s at 4 GiB = %v, %v", name, got, err)
			}
		}
		if !in.WriteUint32Le(0xfffffffc, 0x12345678) {
			t.Fatal("last word of grown 4 GiB memory is inaccessible")
		}
		if got, ok := in.ReadUint32Le(0xfffffffc); !ok || got != 0x12345678 {
			t.Fatalf("last word = %x, %t", got, ok)
		}
		if got, err := in.Invoke("grow", 1); err != nil || !slices.Equal(got, []uint64{0xffffffff}) {
			t.Fatalf("grow beyond 4 GiB = %v, %v", got, err)
		}
	}
}

func TestThreadsSharedMemoryGrowAndAccessSameActivation(t *testing.T) {
	cfg := NewRuntimeConfig().WithFeature(CoreFeatureThreads, true).WithBoundsChecks(BoundsChecksExplicit)
	c, err := cfg.Compile(threadsGrowModule(false, 1, 3))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, compiled := range []*Compiled{c, publicArtifactRoundTrip(t, c)} {
		in, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		got, err := in.Invoke("touch", 1, 65536)
		if err != nil || !slices.Equal(got, []uint64{1, 255}) {
			t.Fatalf("grow/fill/copy/load = %v, %v", got, err)
		}
	}
}

func TestThreadsSharedMemoryConcurrentGrowth(t *testing.T) {
	cfg := NewRuntimeConfig().WithFeature(CoreFeatureThreads, true).WithBoundsChecks(BoundsChecksExplicit)
	c, err := cfg.Compile(threadsGrowModule(true, 1, 65))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c = publicArtifactRoundTrip(t, c)
	mem, err := NewSharedMemory(1, 65)
	if err != nil {
		t.Fatal(err)
	}
	defer mem.Close()
	results := make(chan uint64, 128)
	errors := make(chan error, 8)
	var wg sync.WaitGroup
	for k := 0; k < 8; k++ {
		in, err := Instantiate(c, testImports("env.memory", mem))
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < 16; n++ {
				got, err := in.Invoke("grow", 1)
				if err != nil || len(got) != 1 {
					errors <- fmt.Errorf("grow = %v, %v", got, err)
					return
				}
				results <- got[0]
			}
		}()
	}
	wg.Wait()
	close(results)
	close(errors)
	for err := range errors {
		t.Fatal(err)
	}
	seen := make(map[uint64]bool)
	failed := 0
	for previous := range results {
		if previous == 0xffffffff {
			failed++
			continue
		}
		if previous < 1 || previous > 64 || seen[previous] {
			t.Fatalf("nonlinear growth result %d", previous)
		}
		seen[previous] = true
	}
	if len(seen) != 64 || failed != 64 || len(mem.UnsafeBytes()) != 65*65536 {
		t.Fatalf("successful=%d failed=%d bytes=%d", len(seen), failed, len(mem.UnsafeBytes()))
	}
}

func TestThreadsSharedGrowthVisibleDuringOtherActivation(t *testing.T) {
	cfg := NewRuntimeConfig().WithFeature(CoreFeatureThreads, true).WithBoundsChecks(BoundsChecksExplicit)
	c, err := cfg.Compile(threadsGrowModule(true, 1, 3))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	mem, err := NewSharedMemory(1, 3)
	if err != nil {
		t.Fatal(err)
	}
	defer mem.Close()
	grower, err := Instantiate(c, testImports("env.memory", mem))
	if err != nil {
		t.Fatal(err)
	}
	defer grower.Close()
	observer, err := Instantiate(publicArtifactRoundTrip(t, c), testImports("env.memory", mem))
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		got, err := observer.InvokeContext(ctx, "observe", 2, 200000000)
		if err == nil && !slices.Equal(got, []uint64{2}) {
			err = fmt.Errorf("observer retained stale size: %v", got)
		}
		done <- err
	}()
	flag := (*uint32)(unsafe.Pointer(&mem.UnsafeBytes()[0]))
	for atomic.LoadUint32(flag) == 0 {
		select {
		case err := <-done:
			t.Fatalf("observer exited before growth: %v", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		default:
			time.Sleep(time.Millisecond)
		}
	}
	if got, err := grower.Invoke("grow", 1); err != nil || !slices.Equal(got, []uint64{1}) {
		t.Fatalf("grow = %v, %v", got, err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
