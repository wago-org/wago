//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func threadsSharedSecondaryModule(primaryShared, primary64, secondary64, imported bool) []byte {
	p, q := wasm.I32, wasm.I32
	lp, lq := byte(1), byte(3)
	if primaryShared {
		lp |= 2
	}
	if primary64 {
		p, lp = wasm.I64, lp|4
	}
	if secondary64 {
		q, lq = wasm.I64, lq|4
	}
	count := wasm.I32
	if primary64 && secondary64 {
		count = wasm.I64
	}
	constq := byte(0x41)
	if secondary64 {
		constq = 0x42
	}
	var types, funcs, bodies [][]byte
	exports := [][]byte{wasmtest.ExportEntry("memory0", 2, 0), wasmtest.ExportEntry("memory1", 2, 1)}
	add := func(name string, params, results []wasm.ValType, body []byte) {
		idx := uint32(len(funcs))
		types = append(types, wasmtest.FuncType(params, results))
		funcs = append(funcs, wasmtest.ULEB(idx))
		exports = append(exports, wasmtest.ExportEntry(name, 0, idx))
		bodies = append(bodies, wasmtest.Code(append(body, 0x0b)))
	}
	add("store1", []wasm.ValType{q, wasm.I32}, []wasm.ValType{wasm.I32}, []byte{0x20, 0, 0x20, 1, 0xfe, 0x17, 0x42, 1, 0, 0x20, 0, 0xfe, 0x10, 0x42, 1, 0})
	add("load1", []wasm.ValType{q}, []wasm.ValType{wasm.I32}, []byte{0x20, 0, 0x28, 0x42, 1, 0})
	add("size1", nil, []wasm.ValType{q}, []byte{0x3f, 1})
	add("grow1", []wasm.ValType{q}, []wasm.ValType{q}, []byte{0x20, 0, 0x40, 1})
	add("growzero1", nil, []wasm.ValType{q}, []byte{constq, 0, 0x40, 1})
	add("size0", nil, []wasm.ValType{p}, []byte{0x3f, 0})
	add("grow0", []wasm.ValType{p}, []wasm.ValType{p}, []byte{0x20, 0, 0x40, 0})
	add("notify0", []wasm.ValType{p, wasm.I32}, []wasm.ValType{wasm.I32}, []byte{0x20, 0, 0x20, 1, 0xfe, 0, 2, 0})
	add("notify1", []wasm.ValType{q, wasm.I32}, []wasm.ValType{wasm.I32}, []byte{0x20, 0, 0x20, 1, 0xfe, 0, 0x42, 1, 0})
	add("wait1", []wasm.ValType{q, wasm.I32, wasm.I64}, []wasm.ValType{wasm.I32}, []byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfe, 1, 0x42, 1, 0})
	add("waitzero1", []wasm.ValType{q, wasm.I32}, []wasm.ValType{wasm.I32}, []byte{0x20, 0, 0x20, 1, 0x42, 0, 0xfe, 1, 0x42, 1, 0})
	add("copy01", []wasm.ValType{p, q, count}, nil, []byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 10, 0, 1})
	add("fill1", []wasm.ValType{q, wasm.I32, q}, nil, []byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 11, 1})
	add("fillconst1", []wasm.ValType{q, wasm.I32}, nil, []byte{0x20, 0, 0x20, 1, constq, 33, 0xfc, 11, 1})
	add("init1", []wasm.ValType{q, wasm.I32, wasm.I32}, nil, []byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 8, 1, 1})
	add("initconst1", []wasm.ValType{q, wasm.I32}, nil, []byte{0x20, 0, 0x20, 1, 0x41, 33, 0xfc, 8, 1, 1})
	add("drop", nil, nil, []byte{0xfc, 9, 1})
	add("simd1", []wasm.ValType{q, wasm.I32}, []wasm.ValType{wasm.I32}, []byte{0x20, 0, 0x20, 1, 0xfd, 0x11, 0xfd, 0x0b, 0x44, 1, 0, 0x20, 0, 0xfd, 0, 0x44, 1, 0, 0xfd, 0x1b, 3})
	// Read growth inside one live invocation, without a directory refresh.
	add("observe-grow", nil, []wasm.ValType{wasm.I32}, []byte{constq, 4, 0x41, 1, 0xfe, 0x17, 0x42, 1, 0, 0x03, 0x40, 0x3f, 1, constq, 2, func() byte {
		if secondary64 {
			return 0x54
		}
		return 0x49
	}(), 0x0d, 0, 0x0b, constq, 0x80, 0x80, 4, 0x28, 0x42, 1, 0})
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(types...))}
	if imported {
		m0 := append(wasmtest.Name("env"), wasmtest.Name("memory")...)
		m1 := append(wasmtest.Name("env"), wasmtest.Name("secondary")...)
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(append(m0, 2, lp, 1, 4), append(m1, 2, lq, 1, 4))))
	}
	sections = append(sections, wasmtest.Section(3, wasmtest.Vec(funcs...)))
	if !imported {
		sections = append(sections, wasmtest.Section(5, wasmtest.Vec([]byte{lp, 1, 4}, []byte{lq, 1, 4})))
	}
	sections = append(sections, wasmtest.Section(7, wasmtest.Vec(exports...)), wasmtest.Section(12, []byte{2}), wasmtest.Section(10, wasmtest.Vec(bodies...)))
	active := []byte{2, 1, constq, 0, 0x0b, 4, 21, 0, 0, 0}
	passive := []byte{1, 64}
	for j := byte(1); j <= 64; j++ {
		passive = append(passive, j)
	}
	return wasmtest.Module(append(sections, wasmtest.Section(11, wasmtest.Vec(active, passive)))...)
}

func TestThreadsSharedSecondaryGrowthBulkAndWait(t *testing.T) {
	requireNativeTestFeatures(t, CoreFeatureMultiMemory)
	for _, primaryShared := range []bool{false, true} {
		for _, primary64 := range []bool{false, true} {
			for _, secondary64 := range []bool{false, true} {
				for _, imported := range []bool{false, true} {
					for _, regABI := range []bool{false, true} {
						t.Run(fmt.Sprintf("shared0=%t/p64=%t/q64=%t/imported=%t/regabi=%t", primaryShared, primary64, secondary64, imported, regABI), func(t *testing.T) {
							cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3|CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit).WithOptimization("reg-abi", regABI)
							c, err := cfg.Compile(threadsSharedSecondaryModule(primaryShared, primary64, secondary64, imported))
							if err != nil {
								t.Fatal(err)
							}
							defer c.Close()
							for _, compiled := range []*Compiled{c, publicArtifactRoundTrip(t, c)} {
								imports := NewImports()
								var provider *Instance
								if imported {
									pc, err := cfg.Compile(threadsSharedSecondaryModule(primaryShared, primary64, secondary64, false))
									if err != nil {
										t.Fatal(err)
									}
									defer pc.Close()
									provider, err = Instantiate(pc)
									if err != nil {
										t.Fatal(err)
									}
									defer provider.Close()
									m0, _ := provider.ExportedMemory("memory0")
									m1, _ := provider.ExportedMemory("memory1")
									imports.Memory("env", "memory", m0).Memory("env", "secondary", m1)
								}
								in, err := Instantiate(compiled, imports)
								if err != nil {
									t.Fatal(err)
								}
								defer in.Close()
								check := func(name string, want uint64, args ...uint64) {
									t.Helper()
									got, err := in.Invoke(name, args...)
									if err != nil || len(got) != 1 || got[0] != want {
										t.Fatalf("%s=%v,%v want %d", name, got, err, want)
									}
								}
								check("load1", 21, 0)
								check("waitzero1", 1, 0, 0)
								check("waitzero1", 2, 0, 21)
								check("wait1", 2, 0, 21, 0)
								check("growzero1", 1)
								check("grow1", 1, 1)
								check("size1", 2)
								check("size0", 1)
								check("grow0", 1, 1)
								if provider != nil {
									got, err := provider.Invoke("size1")
									if err != nil || got[0] != 2 {
										t.Fatalf("provider size=%v,%v", got, err)
									}
								}
								check("store1", 37, 65536, 37)
								check("simd1", 43, 65552, 43)
								m0, _ := in.ExportedMemory("memory0")
								m1, _ := in.ExportedMemory("memory1")
								for _, name := range []string{"fill1", "fillconst1"} {
									args := []uint64{65537, 9}
									if name == "fill1" {
										args = append(args, 33)
									}
									if _, err := in.Invoke(name, args...); err != nil {
										t.Fatal(err)
									}
									if !bytes.Equal(m1.UnsafeBytes()[65537:65570], bytes.Repeat([]byte{9}, 33)) {
										t.Fatal("secondary fill contents")
									}
								}
								for _, name := range []string{"init1", "initconst1"} {
									args := []uint64{65537, 0}
									if name == "init1" {
										args = append(args, 33)
									}
									if _, err := in.Invoke(name, args...); err != nil {
										t.Fatal(err)
									}
									for j, v := range m1.UnsafeBytes()[65537:65570] {
										if v != byte(j+1) {
											t.Fatal("secondary passive data contents")
										}
									}
								}
								if _, err := in.Invoke("copy01", 32, 65537, 33); err != nil {
									t.Fatal(err)
								}
								if !bytes.Equal(m0.UnsafeBytes()[32:65], m1.UnsafeBytes()[65537:65570]) {
									t.Fatal("cross-shared copy contents")
								}
								for _, call := range []struct {
									name string
									args []uint64
								}{{"copy01", []uint64{131071, 65537, 33}}, {"fillconst1", []uint64{131071, 9}}, {"initconst1", []uint64{131071, 0}}} {
									before0, before1 := bytes.Clone(m0.UnsafeBytes()), bytes.Clone(m1.UnsafeBytes())
									_, err := in.Invoke(call.name, call.args...)
									var trap *TrapError
									if !errors.As(err, &trap) || trap.Code != TrapLinMemOutOfBounds {
										t.Fatalf("%s invalid range: %v", call.name, err)
									}
									if !bytes.Equal(before0, m0.UnsafeBytes()) || !bytes.Equal(before1, m1.UnsafeBytes()) {
										t.Fatal("trapping bulk operation changed backing")
									}
								}
								if secondary64 {
									check("grow1", ^uint64(0), 1<<32)
								}
								check("growzero1", 2)
								if _, err := in.Invoke("drop"); err != nil {
									t.Fatal(err)
								}
								if _, err := in.Invoke("initconst1", 0, 0); err == nil {
									t.Fatal("dropped data initialized")
								}
							}
						})
					}
				}
			}
		}
	}
}

func TestThreadsSharedSecondaryBlockingWaitAndCancellation(t *testing.T) {
	requireNativeTestFeatures(t, CoreFeatureMultiMemory)
	for _, primaryShared := range []bool{false, true} {
		for _, secondary64 := range []bool{false, true} {
			t.Run(fmt.Sprintf("shared0=%t/q64=%t", primaryShared, secondary64), func(t *testing.T) {
				cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit)
				pc, err := cfg.Compile(threadsSharedSecondaryModule(primaryShared, false, secondary64, false))
				if err != nil {
					t.Fatal(err)
				}
				defer pc.Close()
				producer, err := Instantiate(pc)
				if err != nil {
					t.Fatal(err)
				}
				defer producer.Close()
				m0, _ := producer.ExportedMemory("memory0")
				m1, _ := producer.ExportedMemory("memory1")
				c, err := cfg.Compile(threadsSharedSecondaryModule(primaryShared, false, secondary64, true))
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				a, err := Instantiate(c, NewImports().Memory("env", "memory", m0).Memory("env", "secondary", m1))
				if err != nil {
					t.Fatal(err)
				}
				defer a.Close()
				b, err := Instantiate(c, NewImports().Memory("env", "memory", m0).Memory("env", "secondary", m1))
				if err != nil {
					t.Fatal(err)
				}
				defer b.Close()
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				done := make(chan error, 1)
				go func() {
					got, err := a.InvokeContext(ctx, "wait1", 0, 21, ^uint64(0))
					if err == nil && (len(got) != 1 || got[0] != 0) {
						err = fmt.Errorf("wait result=%v", got)
					}
					done <- err
				}()
				for {
					if got, err := b.Invoke("notify0", 0, 1); err != nil || got[0] != 0 {
						t.Fatalf("primary notified a secondary waiter: %v,%v", got, err)
					}
					got, err := b.Invoke("notify1", 0, 1)
					if err != nil {
						t.Fatal(err)
					}
					if got[0] == 1 {
						break
					}
					select {
					case err := <-done:
						t.Fatalf("wait finished early: %v", err)
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					case <-time.After(time.Millisecond):
					}
				}
				if err := <-done; err != nil {
					t.Fatal(err)
				}
				canceled, stop := context.WithCancel(context.Background())
				done = make(chan error, 1)
				go func() { _, err := a.InvokeContext(canceled, "wait1", 0, 21, ^uint64(0)); done <- err }()
				stop()
				select {
				case err := <-done:
					if err == nil {
						t.Fatal("cancelled wait succeeded")
					}
				case <-time.After(3 * time.Second):
					t.Fatal("cancelled wait hung")
				}
				if got := binary.LittleEndian.Uint32(m0.UnsafeBytes()); got != 0 {
					t.Fatalf("primary wait backing changed: %d", got)
				}
			})
		}
	}
}

func TestThreadsSharedSecondaryGrowthVisibleWithinNativeActivation(t *testing.T) {
	requireNativeTestFeatures(t, CoreFeatureMultiMemory)
	for _, primaryShared := range []bool{false, true} {
		for _, secondary64 := range []bool{false, true} {
			t.Run(fmt.Sprintf("shared0=%t/q64=%t", primaryShared, secondary64), func(t *testing.T) {
				cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit)
				c, err := cfg.Compile(threadsSharedSecondaryModule(primaryShared, false, secondary64, false))
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				in, err := Instantiate(c)
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				if !in.usesPrivateThreadedExecution() {
					t.Fatal("private secondary-shared control uses the global execution lease")
				}
				memory := in.memoryDir.memories[1]
				flag := (*uint32)(unsafe.Pointer(&memory.UnsafeBytes()[4]))
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				done := make(chan error, 1)
				go func() {
					got, err := in.InvokeContext(ctx, "observe-grow")
					if err == nil && (len(got) != 1 || got[0] != 0) {
						err = fmt.Errorf("new shared page=%v", got)
					}
					done <- err
				}()
				for atomic.LoadUint32(flag) == 0 {
					select {
					case err := <-done:
						t.Fatalf("observation finished early: %v", err)
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					case <-time.After(time.Millisecond):
					}
				}
				// The actual guest has already entered with a one-page directory.
				// GrowShared publishes the backing extent without modifying that directory.
				if got := memory.growShared(1, 0); got != 1 {
					t.Fatalf("growShared=%d", got)
				}
				if err := <-done; err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestThreadsAliasedSharedMemoriesUseOneWaitQueue(t *testing.T) {
	requireNativeTestFeatures(t, CoreFeatureMultiMemory)
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit)
	c, err := cfg.Compile(threadsSharedSecondaryModule(true, false, false, true))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	memory, err := NewSharedMemory(1, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer memory.Close()
	imports := NewImports().Memory("env", "memory", memory).Memory("env", "secondary", memory)
	a, err := Instantiate(c, imports)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := Instantiate(c, imports)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		got, err := a.InvokeContext(ctx, "wait1", 0, 21, ^uint64(0))
		if err == nil && (len(got) != 1 || got[0] != 0) {
			err = fmt.Errorf("wait=%v", got)
		}
		done <- err
	}()
	for {
		got, err := b.Invoke("notify0", 0, 1)
		if err != nil {
			t.Fatal(err)
		}
		if got[0] == 1 {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("wait ended early: %v", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(time.Millisecond):
		}
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got, err := b.Invoke("grow1", 1); err != nil || got[0] != 1 {
		t.Fatalf("aliased growth=%v,%v", got, err)
	}
	if got, err := a.Invoke("size0"); err != nil || got[0] != 2 {
		t.Fatalf("aliased primary size=%v,%v", got, err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if got, err := b.Invoke("store1", 65536, 37); err != nil || got[0] != 37 {
		t.Fatalf("closing one importer detached its live alias: %v,%v", got, err)
	}
}
