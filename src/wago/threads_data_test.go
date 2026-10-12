//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"bytes"
	"fmt"
	"sync"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func threadsDataModule(imported bool, activeOffset int32, count int) []byte {
	return threadsDataModuleForAddress(imported, activeOffset, count, false)
}

func threadsDataModuleForAddress(imported bool, activeOffset int32, count int, address64 bool) []byte {
	addressType, constantOp, limits := wasm.I32, byte(0x41), byte(3)
	if address64 {
		addressType = wasm.I64
		constantOp = 0x42
		limits = 7
	}
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(
		wasmtest.FuncType([]wasm.ValType{addressType, wasm.I32, wasm.I32}, nil),
		wasmtest.FuncType(nil, nil),
		wasmtest.FuncType([]wasm.ValType{addressType}, []wasm.ValType{addressType}),
	))}
	if imported {
		entry := append(wasmtest.Name("env"), wasmtest.Name("memory")...)
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(append(entry, 2, limits, 1, 3))))
	}
	sections = append(sections, wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{1}, []byte{2}, []byte{1})))
	if !imported {
		sections = append(sections, wasmtest.Section(5, wasmtest.Vec([]byte{limits, 1, 3})))
	}
	initBody := []byte{0x20, 0, 0x20, 1}
	if count < 0 {
		initBody = append(initBody, 0x20, 2)
	} else {
		initBody = append(initBody, 0x41)
		initBody = append(initBody, wasmtest.SLEB32(int32(count))...)
	}
	initBody = append(initBody, 0xfc, 8, 0, 0, 0x0b)
	sections = append(sections,
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("init", 0, 0), wasmtest.ExportEntry("drop", 0, 1), wasmtest.ExportEntry("grow", 0, 2))),
		// The start function verifies active data was initialized on the actual backing.
		wasmtest.Section(8, []byte{3}),
		wasmtest.Section(12, []byte{3}),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(initBody),
			wasmtest.Code([]byte{0xfc, 9, 0, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x40, 0, 0x0b}),
			wasmtest.Code([]byte{constantOp, 8, 0x2d, 0, 0, 0x41, 0x11, 0x47, 0x04, 0x40, 0x00, 0x0b, 0x0b}),
		)),
	)
	active := func(off int32, data []byte) []byte {
		result := append([]byte{0, constantOp}, wasmtest.SLEB32(off)...)
		result = append(result, 0x0b)
		result = append(result, wasmtest.ULEB(uint32(len(data)))...)
		return append(result, data...)
	}
	sections = append(sections, wasmtest.Section(11, wasmtest.Vec(
		append([]byte{1, 64}, threadsDataSeed()...),
		active(8, []byte{0x11, 0x22, 0x33, 0x44}),
		active(activeOffset, []byte{0x55, 0x66, 0x77}),
	)))
	return wasmtest.Module(sections...)
}

func TestThreadsSharedDataExecutionAndCodec(t *testing.T) {
	cfg := NewRuntimeConfig().WithFeature(CoreFeatureThreads, true).WithBoundsChecks(BoundsChecksExplicit)
	for _, imported := range []bool{false, true} {
		t.Run(fmt.Sprintf("imported=%t", imported), func(t *testing.T) {
			c, err := cfg.Compile(threadsDataModule(imported, 10, -1))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			for _, compiled := range []*Compiled{c, publicArtifactRoundTrip(t, c)} {
				opts := InstantiateOptions{}
				if imported {
					memory, err := NewSharedMemory(1, 3)
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
				if got := in.mem()[8:13]; !bytes.Equal(got, []byte{0x11, 0x22, 0x55, 0x66, 0x77}) {
					t.Fatalf("active order: %x", got)
				}
				source := threadsDataSeed()
				for _, args := range [][3]uint64{{32, 0, 8}, {33, 1, 7}, {65535, 7, 1}, {65536, 8, 0}, {65536, 0, 1}, {20, 8, 1}, {20, 9, 0}, {0xffffffff, 0, 1}, {1<<32 | 31, 1<<32 | 2, 1<<32 | 3}} {
					before := bytes.Clone(in.mem())
					dst, src, n := uint64(uint32(args[0])), uint64(uint32(args[1])), uint64(uint32(args[2]))
					valid := dst+n <= uint64(len(before)) && src+n <= uint64(len(source))
					want := bytes.Clone(before)
					if valid {
						copy(want[dst:dst+n], source[src:src+n])
					}
					_, err := in.Invoke("init", args[0], args[1], args[2])
					if valid && err != nil || !valid && err == nil {
						t.Fatalf("init %v: %v, valid=%t", args, err, valid)
					}
					if !bytes.Equal(in.mem(), want) {
						t.Fatalf("init %v modified incorrect bytes", args)
					}
				}
				if _, err := in.Invoke("grow", 1); err != nil {
					t.Fatal(err)
				}
				if _, err := in.Invoke("init", 65539, 0, 8); err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(in.mem()[65539:65547], source[:8]) {
					t.Fatal("init did not use grown backing")
				}
				if _, err := in.Invoke("drop"); err != nil {
					t.Fatal(err)
				}
				if _, err := in.Invoke("init", 131072, 0, 0); err != nil {
					t.Fatal(err)
				}
				for _, args := range [][3]uint64{{0, 0, 1}, {0, 1, 0}} {
					if _, err := in.Invoke("init", args[0], args[1], args[2]); err == nil {
						t.Fatalf("dropped init %v succeeded", args)
					}
				}
				// Descriptors belong to each instance, even when both instances share memory.
				other, err := Instantiate(compiled, opts)
				if err != nil {
					t.Fatal(err)
				}
				defer other.Close()
				if _, err := other.Invoke("init", 40, 0, 8); err != nil {
					t.Fatalf("drop leaked into another instance: %v", err)
				}
			}
		})
	}
}

func TestThreadsSharedActiveDataFailureReleasesMemory(t *testing.T) {
	cfg := NewRuntimeConfig().WithFeature(CoreFeatureThreads, true).WithBoundsChecks(BoundsChecksExplicit)
	c, err := cfg.Compile(threadsDataModule(true, 65535, -1))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	memory, err := NewSharedMemory(1, 3)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Instantiate(c, testImports("env.memory", memory)); err == nil {
		t.Fatal("out-of-bounds active segment succeeded")
	}
	// Earlier segments remain visible, but the failing segment writes no bytes.
	if got := memory.UnsafeBytes(); !bytes.Equal(got[8:12], []byte{0x11, 0x22, 0x33, 0x44}) || got[65535] != 0 {
		t.Fatal("failed segment initialization changed wrong bytes")
	}
	if err := memory.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestThreadsSharedActiveDataConcurrentGrowth(t *testing.T) {
	cfg := NewRuntimeConfig().WithFeature(CoreFeatureThreads, true).WithBoundsChecks(BoundsChecksExplicit)
	c, err := cfg.Compile(threadsDataModule(true, 10, -1))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	growth, err := cfg.Compile(threadsGrowModule(true, 1, 3))
	if err != nil {
		t.Fatal(err)
	}
	defer growth.Close()
	memory, err := NewSharedMemory(1, 3)
	if err != nil {
		t.Fatal(err)
	}
	defer memory.Close()
	opts := InstantiateOptions{Imports: testImports("env.memory", memory)}
	grower, err := Instantiate(growth, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer grower.Close()
	var wg sync.WaitGroup
	errors := make(chan error, 65)
	start := make(chan struct{})
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for i := 0; i < 8; i++ {
				in, err := Instantiate(c, opts)
				if err != nil {
					errors <- err
					return
				}
				if err := in.Close(); err != nil {
					errors <- err
					return
				}
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 2; i++ {
			if _, err := grower.Invoke("grow", 1); err != nil {
				errors <- err
				return
			}
		}
	}()
	close(start)
	wg.Wait()
	close(errors)
	for err := range errors {
		t.Fatal(err)
	}
	if got := memory.UnsafeBytes(); len(got) != 3*65536 || !bytes.Equal(got[8:13], []byte{0x11, 0x22, 0x55, 0x66, 0x77}) {
		t.Fatal("concurrent initialization or growth lost published state")
	}
}

func threadsDataSeed() []byte {
	source := make([]byte, 64)
	for i := range source {
		source[i] = byte(2*i + 1)
	}
	return source
}

func TestThreadsSharedDataConstantLengths(t *testing.T) {
	cfg := NewRuntimeConfig().WithFeature(CoreFeatureThreads, true).WithBoundsChecks(BoundsChecksExplicit)
	for _, n := range []int{0, 1, 2, 3, 4, 7, 8, 9, 15, 16, 17, 31, 32, 33, 63, 64, 65} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			c, err := cfg.Compile(threadsDataModule(false, 10, n))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			for _, compiled := range []*Compiled{c, publicArtifactRoundTrip(t, c)} {
				in, err := Instantiate(compiled, nil)
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				for _, pair := range [][2]uint64{{33, 0}, {33, 1}, {33, 64}, {33, 65}, {65536 - uint64(n), 0}, {65537 - uint64(n), 0}} {
					want := bytes.Clone(in.mem())
					dst, src := pair[0], pair[1]
					valid := dst+uint64(n) <= uint64(len(want)) && src+uint64(n) <= 64
					if valid {
						copy(want[dst:dst+uint64(n)], threadsDataSeed()[src:src+uint64(n)])
					}
					_, err := in.Invoke("init", dst, src, 0)
					if valid && err != nil || !valid && err == nil {
						t.Fatalf("n=%d dst=%d src=%d valid=%t: %v", n, dst, src, valid, err)
					}
					if !bytes.Equal(want, in.mem()) {
						t.Fatal("constant init wrote incorrect bytes")
					}
				}
			}
		})
	}
}

func threadsDataPressureModule() []byte {
	// Keep sixteen computed scalar locals, twenty-four vector locals and eight
	// prior memory loads live across a constant initialization that overwrites
	// some of those loads' addresses. This checks pins, spills and load ordering.
	body := []byte{2, 16, 0x7f, 24, 0x7b}
	for i := byte(0); i < 16; i++ {
		body = append(body, 0x20, 1, 0x41, i+1, 0x6a, 0x21, i+2)
	}
	for i := byte(0); i < 24; i++ {
		body = append(body, 0x20, 1, 0x41, i+17, 0x6a, 0xfd, 17, 0x21, i+18)
	}
	for i := byte(0); i < 8; i++ {
		body = append(body, 0x41, i*8, 0x29, 3, 0)
	}
	body = append(body, 0x20, 0, 0x20, 1, 0x41, 33, 0xfc, 8, 0, 0)
	for i := 0; i < 7; i++ {
		body = append(body, 0x7c)
	}
	for i := byte(0); i < 16; i++ {
		body = append(body, 0x20, i+2, 0xad, 0x7c)
	}
	for i := byte(0); i < 24; i++ {
		body = append(body, 0x20, i+18, 0xfd, 27, 0, 0xad, 0x7c)
	}
	body = append(body, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(5, wasmtest.Vec([]byte{3, 1, 3})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(12, []byte{1}),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
		wasmtest.Section(11, wasmtest.Vec(append([]byte{1, 64}, threadsDataSeed()...))),
	)
}

func TestThreadsSharedDataLiveValuesAndCodec(t *testing.T) {
	for _, cache := range []bool{false, true} {
		for _, regABI := range []bool{false, true} {
			t.Run(fmt.Sprintf("cache=%t/regabi=%t", cache, regABI), func(t *testing.T) {
				cfg := NewRuntimeConfig().WithFeature(CoreFeatureThreads, true).WithBoundsChecks(BoundsChecksExplicit).
					WithOptimization("shared-memory-base", cache).WithOptimization("reg-abi", regABI)
				c, err := cfg.Compile(threadsDataPressureModule())
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				for _, compiled := range []*Compiled{c, publicArtifactRoundTrip(t, c)} {
					in, err := Instantiate(compiled, nil)
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					for i := uint32(0); i < 8; i++ {
						if !in.WriteUint64Le(i*8, uint64(i+100)) {
							t.Fatal("initialize live loads")
						}
					}
					result, err := in.Invoke("run", 32, 0)
					if err != nil || len(result) != 1 || result[0] != 1648 {
						t.Fatalf("live values: %v, %v; want 1648", result, err)
					}
					if !bytes.Equal(in.mem()[32:65], threadsDataSeed()[:33]) {
						t.Fatal("wrong initialized bytes")
					}
				}
			})
		}
	}
}
