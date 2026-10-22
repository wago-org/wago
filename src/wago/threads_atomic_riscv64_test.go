//go:build linux && riscv64 && !tinygo && !wago_target_tinygo && !wago_precompiled

package wago

import (
	"context"
	"encoding/binary"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64AtomicModule(params []wasm.ValType, results []wasm.ValType, sub, align, offset byte) []byte {
	imp := append(wasmtest.Name("env"), wasmtest.Name("memory")...)
	imp = append(imp, 2, 3, 1, 2)
	body := []byte{}
	for i := range params {
		body = append(body, 0x20, byte(i))
	}
	body = append(body, 0xfe, sub, align, offset, 0x0b)
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, results))), wasmtest.Section(2, wasmtest.Vec(imp)), wasmtest.Section(3, wasmtest.Vec([]byte{0})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
}

func rv64CompileAtomic(t *testing.T, data []byte) *Compiled {
	t.Helper()
	cfg := compatibilityDefaultConfig().WithBoundsChecks(BoundsChecksExplicit)
	features := cfg.frontendFeatures()
	features.Threads = true
	c, err := compileWithFrontendFeatures(cfg, data, features)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func rv64AtomicInstance(t *testing.T, data []byte, memory *Memory) *Instance {
	t.Helper()
	c := rv64CompileAtomic(t, data)
	in, err := Instantiate(c, testImports("env.memory", memory))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = in.Close() })
	return in
}

func TestRV64AtomicRMWWidthsAndLanes(t *testing.T) {
	for _, tc := range []struct {
		sub, align byte
		typ        wasm.ValType
		size       int
	}{{0x1e, 2, wasm.I32, 4}, {0x1f, 3, wasm.I64, 8}, {0x20, 0, wasm.I32, 1}, {0x21, 1, wasm.I32, 2}, {0x22, 0, wasm.I64, 1}, {0x23, 1, wasm.I64, 2}, {0x24, 2, wasm.I64, 4}} {
		t.Run(fmt.Sprintf("%x", tc.sub), func(t *testing.T) {
			memory, err := NewSharedMemory(1, 2)
			if err != nil {
				t.Fatal(err)
			}
			defer memory.Close()
			in := rv64AtomicInstance(t, rv64AtomicModule([]wasm.ValType{wasm.I32, tc.typ}, []wasm.ValType{tc.typ}, tc.sub, tc.align, 0), memory)
			data := memory.UnsafeBytes()
			for i := 0; i < 16; i++ {
				data[i] = 0xa5
			}
			address := uint64(8)
			if tc.size == 1 {
				address = 11
			}
			if tc.size == 2 {
				address = 10
			}
			initial := uint64(0)
			for i := 0; i < tc.size; i++ {
				initial |= uint64(data[int(address)+i]) << uint(8*i)
			}
			mask := ^uint64(0)
			if tc.size < 8 {
				mask = (uint64(1) << uint(8*tc.size)) - 1
			}
			got, err := in.Invoke("run", 0xdeadbeef00000000|address, uint64(1))
			if err != nil || len(got) != 1 || got[0] != initial {
				t.Fatal(got, err, initial)
			}
			after := uint64(0)
			for i := 0; i < tc.size; i++ {
				after |= uint64(data[int(address)+i]) << uint(8*i)
			}
			if after != (initial+1)&mask {
				t.Fatal(after, initial)
			}
			for i := 0; i < 16; i++ {
				if (i < int(address) || i >= int(address)+tc.size) && data[i] != 0xa5 {
					t.Fatal("neighbor changed", i)
				}
			}
		})
	}
}

func TestRV64AtomicCompareExchangeAndContention(t *testing.T) {
	memory, err := NewSharedMemory(1, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer memory.Close()
	source := rv64AtomicModule([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, 0x48, 2, 0)
	in := rv64AtomicInstance(t, source, memory)
	word := (*uint32)(unsafe.Pointer(&memory.UnsafeBytes()[0]))
	atomic.StoreUint32(word, 0x80000000)
	for _, tc := range []struct{ expected, replacement, want uint64 }{{1, 2, 0x80000000}, {0xffffffff80000000, 3, 0x80000000}, {3, 0xffffffff, 3}} {
		got, err := in.Invoke("run", uint64(0), tc.expected, tc.replacement)
		if err != nil || len(got) != 1 || got[0] != tc.want {
			t.Fatal(got, err, tc)
		}
	}
	const workers, increments = 4, 250
	atomic.StoreUint32(word, 0)
	add := rv64CompileAtomic(t, rv64AtomicModule([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, 0x1e, 2, 0))
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for range workers {
		instance, err := Instantiate(add, testImports("env.memory", memory))
		if err != nil {
			t.Fatal(err)
		}
		defer instance.Close()
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range increments {
				if _, err := instance.Invoke("run", uint64(0), uint64(1)); err != nil {
					errs <- err
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if got := atomic.LoadUint32(word); got != workers*increments {
		t.Fatal(got)
	}
}

func TestRV64AtomicWaitNotifyCancel(t *testing.T) {
	memory, err := NewSharedMemory(1, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer memory.Close()
	binary.LittleEndian.PutUint32(memory.UnsafeBytes(), 7)
	wait := rv64AtomicInstance(t, rv64AtomicModule([]wasm.ValType{wasm.I32, wasm.I32, wasm.I64}, []wasm.ValType{wasm.I32}, 1, 2, 0), memory)
	notify := rv64AtomicInstance(t, rv64AtomicModule([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, 0, 2, 0), memory)
	for _, tc := range []struct{ expected, timeout, want uint64 }{{8, ^uint64(0), 1}, {7, 0, 2}} {
		got, err := wait.Invoke("run", uint64(0), tc.expected, tc.timeout)
		if err != nil || len(got) != 1 || got[0] != tc.want {
			t.Fatal(got, err, tc)
		}
	}
	result := make(chan error, 1)
	go func() {
		got, err := wait.Invoke("run", uint64(0), uint64(7), ^uint64(0))
		if err == nil && (len(got) != 1 || got[0] != 0) {
			t.Errorf("wait=%v", got)
		}
		result <- err
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		got, err := notify.Invoke("run", uint64(0), uint64(1))
		if err != nil {
			t.Fatal(err)
		}
		if got[0] == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("waiter did not park")
		}
		time.Sleep(time.Millisecond)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := wait.InvokeContext(ctx, "run", I32(0), I32(7), I64(-1)); err == nil {
		t.Fatal("wait cancellation failed")
	}
}

func rv64SharedGrowthModule(mixed ...bool) []byte {
	growBody := []byte{0x20, 0, 0x40, 0, 0x0b}
	if len(mixed) != 0 && mixed[0] {
		prefix := append([]byte{0xfd, 0x0c}, make([]byte, 16)...)
		prefix = append(prefix, 0x1a)
		growBody = append(prefix, growBody...)
	}
	imp := append(wasmtest.Name("env"), wasmtest.Name("memory")...)
	imp = append(imp, 2, 3, 1, 8)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("grow", 0, 0), wasmtest.ExportEntry("size", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(growBody), wasmtest.Code([]byte{0x3f, 0, 0x0b}))))
}

func TestRV64SharedGrowthQuotaAndContention(t *testing.T) {
	source := rv64SharedGrowthModule()
	c := rv64CompileAtomic(t, source)
	memory, err := NewSharedMemory(1, 8)
	if err != nil {
		t.Fatal(err)
	}
	defer memory.Close()
	a, err := Instantiate(c, testImports("env.memory", memory))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	opts := InstantiateOptions{Imports: testImports("env.memory", memory), memoryLimitPages: 2}
	limited, err := Instantiate(c, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer limited.Close()
	for _, tc := range []struct{ delta, want uint64 }{{1, 1}, {1, 0xffffffff}, {0, 2}, {0xffffffff, 0xffffffff}} {
		got, err := limited.Invoke("grow", tc.delta)
		if err != nil || len(got) != 1 || got[0] != tc.want {
			t.Fatal(tc, got, err)
		}
	}
	got, err := a.Invoke("size")
	if err != nil || got[0] != 2 {
		t.Fatal(got, err)
	}
	const workers = 4
	var wg sync.WaitGroup
	results := make(chan uint64, workers)
	errs := make(chan error, workers)
	for range workers {
		in, err := Instantiate(c, testImports("env.memory", memory))
		if err != nil {
			t.Fatal(err)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer in.Close()
			got, err := in.Invoke("grow", uint64(1))
			if err != nil {
				errs <- err
				return
			}
			results <- got[0]
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	seen := map[uint64]bool{}
	for result := range results {
		if result < 2 || result >= 6 || seen[result] {
			t.Fatal(result, seen)
		}
		seen[result] = true
	}
	if len(seen) != workers {
		t.Fatal(seen)
	}
	got, err = limited.Invoke("grow", uint64(0))
	if err != nil || got[0] != 6 {
		t.Fatal("zero delta after quota", got, err)
	}
	bytes := memory.UnsafeBytes()
	if len(bytes) != 6*65536 {
		t.Fatal(len(bytes))
	}
	for _, address := range []int{2 * 65536, 6*65536 - 1} {
		if bytes[address] != 0 {
			t.Fatal("grown bytes not zero", address)
		}
	}
}

func rv64MixedAtomicModule(params []wasm.ValType, sub, align byte, store bool) []byte {
	imp := append(wasmtest.Name("env"), wasmtest.Name("memory")...)
	imp = append(imp, 2, 3, 1, 2)
	body := []byte{0xfd, 0x0c}
	value := make([]byte, 16)
	binary.LittleEndian.PutUint64(value[8:], 0x1122334455667788)
	body = append(body, value...)
	for i := range params {
		body = append(body, 0x20, byte(i))
	}
	body = append(body, 0xfe, sub, align, 0)
	if !store {
		body = append(body, 0x1a)
	}
	body = append(body, 0xfd, 0x1d, 1, 0x0b)
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, []wasm.ValType{wasm.I64}))), wasmtest.Section(2, wasmtest.Vec(imp)), wasmtest.Section(3, wasmtest.Vec([]byte{0})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
}

func TestRV64AtomicMixedSWARPreservesLiveVectorAndArtifact(t *testing.T) {
	for _, tc := range []struct {
		name       string
		params     []wasm.ValType
		sub, align byte
		store      bool
		args       []uint64
	}{
		{"add", []wasm.ValType{wasm.I32, wasm.I32}, 0x1e, 2, false, []uint64{0, 1}},
		{"load", []wasm.ValType{wasm.I32}, 0x10, 2, false, []uint64{0}},
		{"store", []wasm.ValType{wasm.I32, wasm.I32}, 0x17, 2, true, []uint64{0, 1}},
		{"wait", []wasm.ValType{wasm.I32, wasm.I32, wasm.I64}, 1, 2, false, []uint64{0, 0, 0}},
		{"notify", []wasm.ValType{wasm.I32, wasm.I32}, 0, 2, false, []uint64{0, 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			memory, err := NewSharedMemory(1, 2)
			if err != nil {
				t.Fatal(err)
			}
			defer memory.Close()
			c := rv64CompileAtomic(t, rv64MixedAtomicModule(tc.params, tc.sub, tc.align, tc.store))
			loaded := publicArtifactRoundTrip(t, c)
			if loaded != c {
				defer loaded.Close()
			}
			for _, code := range []*Compiled{c, loaded} {
				in, err := Instantiate(code, testImports("env.memory", memory))
				if err != nil {
					t.Fatal(err)
				}
				got, err := in.Invoke("run", tc.args...)
				in.Close()
				if err != nil || len(got) != 1 || got[0] != 0x1122334455667788 {
					t.Fatal(got, err)
				}
			}
		})
	}
}

func TestRV64SharedGrowthSWARAndArtifact(t *testing.T) {
	c := rv64CompileAtomic(t, rv64SharedGrowthModule(true))
	loaded := publicArtifactRoundTrip(t, c)
	if loaded != c {
		defer loaded.Close()
	}
	memory, err := NewSharedMemory(1, 8)
	if err != nil {
		t.Fatal(err)
	}
	defer memory.Close()
	for i, code := range []*Compiled{c, loaded} {
		in, err := Instantiate(code, testImports("env.memory", memory))
		if err != nil {
			t.Fatal(err)
		}
		got, err := in.Invoke("grow", uint64(1))
		if err != nil || len(got) != 1 || got[0] != uint64(i+1) {
			t.Fatal(got, err)
		}
		got, err = in.Invoke("size")
		in.Close()
		if err != nil || got[0] != uint64(i+2) {
			t.Fatal(got, err)
		}
	}
}

func TestRV64AtomicVectorParametersAndResults(t *testing.T) {
	imp := append(wasmtest.Name("env"), wasmtest.Name("memory")...)
	imp = append(imp, 2, 3, 1, 2)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.V128, wasm.I32}, []wasm.ValType{wasm.V128}))),
		wasmtest.Section(2, wasmtest.Vec(imp)), wasmtest.Section(3, wasmtest.Vec([]byte{0})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 1, 0x20, 0, 0x20, 2, 0xfe, 0x1e, 2, 0, 0x1a, 0x0b}))))
	c := rv64CompileAtomic(t, source)
	loaded := publicArtifactRoundTrip(t, c)
	if loaded != c {
		defer loaded.Close()
	}
	memory, err := NewSharedMemory(1, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer memory.Close()
	for _, code := range []*Compiled{c, loaded} {
		in, err := Instantiate(code, testImports("env.memory", memory))
		if err != nil {
			t.Fatal(err)
		}
		got, err := in.Invoke("run", uint64(0), uint64(0x1122334455667788), uint64(0x99aabbccddeeff00), uint64(1))
		in.Close()
		if err != nil || len(got) != 2 || got[0] != 0x1122334455667788 || got[1] != 0x99aabbccddeeff00 {
			t.Fatal(got, err)
		}
	}
}

func TestRV64SharedGrowthFullMemory32Boundary(t *testing.T) {
	imp := append(wasmtest.Name("env"), wasmtest.Name("memory")...)
	imp = append(imp, 2, 3, 0)
	imp = append(imp, wasmtest.ULEB(65536)...)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(imp)), wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{0})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("grow", 0, 0), wasmtest.ExportEntry("add", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x40, 0, 0x0b}), wasmtest.Code([]byte{0x20, 0, 0x41, 1, 0xfe, 0x1e, 2, 0, 0x0b}))))
	c := rv64CompileAtomic(t, source)
	memory, err := NewSharedMemory(0, 65536)
	if err != nil {
		t.Fatal(err)
	}
	defer memory.Close()
	in, err := Instantiate(c, testImports("env.memory", memory))
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	got, err := in.Invoke("grow", uint64(65536))
	if err != nil || got[0] != 0 {
		t.Fatal(got, err)
	}
	got, err = in.Invoke("grow", uint64(0))
	if err != nil || got[0] != 65536 {
		t.Fatal(got, err)
	}
	got, err = in.Invoke("grow", uint64(1))
	if err != nil || got[0] != 0xffffffff {
		t.Fatal(got, err)
	}
	got, err = in.Invoke("add", uint64(0xfffffffc))
	if err != nil || got[0] != 0 {
		t.Fatal("last word", got, err)
	}
	data := memory.UnsafeBytes()
	if uint64(len(data)) != 1<<32 || binary.LittleEndian.Uint32(data[len(data)-4:]) != 1 {
		t.Fatal("full byte size", len(data))
	}
}

func TestRV64SharedGrowthConcurrentHostViews(t *testing.T) {
	c := rv64CompileAtomic(t, rv64SharedGrowthModule())
	memory, err := NewSharedMemory(1, 8)
	if err != nil {
		t.Fatal(err)
	}
	defer memory.Close()
	reader, err := Instantiate(c, testImports("env.memory", memory))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	grower, err := Instantiate(c, testImports("env.memory", memory))
	if err != nil {
		t.Fatal(err)
	}
	defer grower.Close()
	done := make(chan struct{})
	fail := make(chan string, 1)
	go func() {
		defer close(done)
		previous := 65536
		for range 1000 {
			b := memory.UnsafeBytes()
			if len(b) < previous || len(b) > 8*65536 {
				fail <- "host view size went backwards or exceeded maximum"
				return
			}
			previous = len(b)
			if _, ok := reader.ReadUint8(0); !ok {
				fail <- "host read lost live shared memory"
				return
			}
		}
	}()
	for old := 1; old < 8; old++ {
		got, err := grower.Invoke("grow", uint64(1))
		if err != nil || got[0] != uint64(old) {
			t.Fatal(got, err)
		}
	}
	<-done
	select {
	case message := <-fail:
		t.Fatal(message)
	default:
	}
	if len(memory.UnsafeBytes()) != 8*65536 {
		t.Fatal("final host view")
	}
}
