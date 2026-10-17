//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestRV64PublicSWARStateHostInterleavedImportsNestedContextAndRetirement(t *testing.T) {
	for _, position := range []int{0, 4, 8} {
		for _, artifact := range []bool{false, true} {
			t.Run(fmt.Sprintf("position%d-artifact%t", position, artifact), func(t *testing.T) {
				memoryProducer, memory := importedMemoryProvider(t)
				globalCode, err := Compile(nil, rv64PublicSWARGlobalExportsModule())
				if err != nil {
					t.Fatal(err)
				}
				defer globalCode.Close()
				globalProducer, err := Instantiate(globalCode)
				if err != nil {
					t.Fatal(err)
				}
				defer globalProducer.Close()
				imports, objects := rv64PublicSWARBorrowState(t, globalProducer, memory)
				source := rv64PublicSWARStateHostModule(2, 3, position)
				c, err := Compile(NewRuntimeConfig().WithMemoryLimitPages(0), source)
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				if c.NumImports != 1 || len(c.Funcs) != 7 || len(c.GlobalImports) != 7 {
					t.Fatal("state imports changed function/global ordinal metadata")
				}
				if artifact {
					blob, err := c.MarshalBinary()
					if err != nil {
						t.Fatal(err)
					}
					loaded, err := LoadTrustedArtifact(blob)
					if loaded != nil {
						defer loaded.Close()
					}
					if err != nil {
						t.Fatal(err)
					}
					if err := c.Close(); err != nil {
						t.Fatal(err)
					}
					c = loaded
				}
				var nested *Instance
				panicHost := false
				cause := errors.New("mixed state parked host panic")
				initial := []uint64{0xffffffff, 0x8000000000000001, 0x7f800001, 0x7ff0000000000001, 0x8899aabbccddeeff, 0x0807060504030201, 0x7ff0000000000001, 0x80000000deadbeef}
				changed := []uint64{0x80000000, 9, 0xff800001, 0xfff0000000000001, 0x80000000deadbeef, 0x7ff0000000000042, initial[6], initial[7]}
				payload := []uint64{0x7ff0000000000001, 0xff8000017f800001}
				hostResults := []uint64{0xdeadbeefffffffff, 0x8000000000000001, 0xaabbccddff800001, 0x7ff0000000000042}
				host := HostCallFunc(func(call HostCall) {
					if panicHost {
						panic(HostTrap{Err: cause})
					}
					if call.ParamCount() != 4 || call.ResultCount() != 4 || !slices.Equal(call.ParamSlots(), []uint64{0x80000000, 0x8000000000000001, 0x7f800001, 0x7ff0000000000001}) {
						panic("mixed state host scalar slots")
					}
					runtime.GC()
					// Both instances use the same imported memory home but different private
					// global directories. Nested guest calls must restore the outer context.
					_, err := nested.Invoke("set", changed[0]|0xdeadbeef00000000, changed[1], changed[2]|0xaabbccdd00000000, changed[3], changed[4], changed[5])
					if err != nil {
						panic(err)
					}
					_, err = nested.Invoke("private", 0xffffffffffffffff, 0x8000000000000000)
					if err != nil {
						panic(err)
					}
					_, err = nested.Invoke("store", 0xdeadbeef00000003, payload[0], payload[1])
					if err != nil {
						panic(err)
					}
					_, err = nested.Invoke("grow", 0xdeadbeef00000000)
					if err != nil {
						panic(err)
					}
					copy(call.ResultSlots(), hostResults)
					runtime.GC()
				})
				imports.Function("env", "host", host)
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
				nested = b
				private := []uint64{0xff8000017f800001, 0x7ff0000000000042}
				importedGlobalInvoke(t, a, "private", []uint64{0, 0}, private...)
				input := []uint64{0xdeadbeef80000000, 0x8000000000000001, 0xaabbccdd7f800001, 0x7ff0000000000001, 0x8899aabbccddeeff, 0x0807060504030201, 0xdeadbeef00000003}
				expected := func(old []uint64) []uint64 {
					values := append(slices.Clone(input[4:6]), old[4], old[5])
					values = append(values, private...)
					values = append(values, uint64(uint32(hostResults[0])), hostResults[1], uint64(uint32(hostResults[2])), hostResults[3])
					values = append(values, payload...)
					values = append(values, changed[4], changed[5])
					return append(values, private...)
				}
				importedGlobalInvoke(t, a, "host", expected(initial), input...)
				importedGlobalInvoke(t, b, "get", rv64PublicSWARImportedGlobalWant(changed, rv64PublicVector(0xffffffffffffffff, 0x8000000000000000)))
				if binary.LittleEndian.Uint64(memory.UnsafeBytes()[3:]) != payload[0] || binary.LittleEndian.Uint64(memory.UnsafeBytes()[11:]) != payload[1] {
					t.Fatal("nested shared memory bytes")
				}
				panicHost = true
				if output, err := a.Invoke("host", input...); output != nil || !errors.Is(err, cause) {
					t.Fatal("parked mixed-state panic identity", output, err)
				}
				panicHost = false
				importedGlobalInvoke(t, a, "host", expected(changed), input...)
				if err := memoryProducer.Close(); err != nil {
					t.Fatal(err)
				}
				if err := globalProducer.Close(); err != nil {
					t.Fatal(err)
				}
				if err := globalCode.Close(); err != nil {
					t.Fatal(err)
				}
				// Code remains open, so late attachment must fail on its retired state.
				if late, err := Instantiate(c, imports); late != nil || err == nil {
					t.Fatal("host/state late attachment to retired owners")
				}
				if err := c.Close(); err != nil {
					t.Fatal(err)
				}
				runtime.GC()
				if memory.jobMemory() == nil || !globalProducer.hasPhysicalResources() || objects[4].GetV128() != (V128{}) {
					t.Fatal("state producers lost parked-host borrowers")
				}
				importedGlobalInvoke(t, a, "host", expected(changed), input...)
				if err := a.Close(); err != nil {
					t.Fatal(err)
				}
				if memory.jobMemory() == nil || !globalProducer.hasPhysicalResources() {
					t.Fatal("first host-state borrower released producers")
				}
				importedGlobalInvoke(t, b, "get", rv64PublicSWARImportedGlobalWant(changed, rv64PublicVector(0xffffffffffffffff, 0x8000000000000000)))
				if err := b.Close(); err != nil {
					t.Fatal(err)
				}
				if memory.jobMemory() != nil || globalProducer.hasPhysicalResources() {
					t.Fatal("final host-state borrower retained producers")
				}
			})
		}
	}
}

func TestRV64PublicSWARStateHostInterruptionAfterNestedSharedHome(t *testing.T) {
	previous := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previous)
	for _, artifact := range []bool{false, true} {
		for _, closeInstance := range []bool{false, true} {
			t.Run(fmt.Sprintf("artifact%t-close%t", artifact, closeInstance), func(t *testing.T) {
				_, memory := importedMemoryProvider(t)
				producerCode, err := Compile(nil, rv64PublicSWARGlobalExportsModule())
				if err != nil {
					t.Fatal(err)
				}
				defer producerCode.Close()
				producer, err := Instantiate(producerCode)
				if err != nil {
					t.Fatal(err)
				}
				defer producer.Close()
				imports, _ := rv64PublicSWARBorrowState(t, producer, memory)
				c, err := Compile(nil, rv64PublicSWARStateHostModule(2, 3, 4))
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				if artifact {
					blob, err := c.MarshalBinary()
					if err != nil {
						t.Fatal(err)
					}
					loaded, err := LoadTrustedArtifact(blob)
					if loaded != nil {
						defer loaded.Close()
					}
					if err != nil {
						t.Fatal(err)
					}
					c = loaded
				}
				entered, release := make(chan struct{}, 1), make(chan struct{})
				var once sync.Once
				unblock := func() { once.Do(func() { close(release) }) }
				defer unblock()
				var b *Instance
				imports.Function("env", "host", HostCallFunc(func(call HostCall) {
					for _, nested := range []struct {
						name string
						args []uint64
					}{{"private", []uint64{1, 2}}, {"store", []uint64{3, 5, 6}}, {"grow", []uint64{0}}} {
						if _, err := b.Invoke(nested.name, nested.args...); err != nil {
							panic(HostTrap{Err: err})
						}
					}
					entered <- struct{}{}
					<-release
					copy(call.ResultSlots(), call.ParamSlots())
				}))
				a, err := Instantiate(c, imports)
				if err != nil {
					t.Fatal(err)
				}
				defer a.Close()
				b, err = Instantiate(c, imports)
				if err != nil {
					t.Fatal(err)
				}
				defer b.Close()
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				type outcome struct {
					values []uint64
					err    error
				}
				done := make(chan outcome, 1)
				joined := false
				defer func() {
					cancel()
					unblock()
					if !joined {
						select {
						case <-done:
						case <-time.After(5 * time.Second):
							t.Error("parked invocation did not join promptly during cleanup")
							// Keep owners live until the invocation actually joins. The
							// test process timeout diagnoses a stuck native bridge.
							<-done
						}
					}
				}()
				args := []uint64{7, 8, 9, 10, 11, 12, 3}
				go func() { values, err := a.InvokeContext(ctx, "host", args...); done <- outcome{values, err} }()
				select {
				case <-entered:
				case <-time.After(5 * time.Second):
					t.Fatal("nested shared-home host did not park")
				}
				if closeInstance {
					closed := make(chan error, 1)
					go func() { closed <- a.Close() }()
					select {
					case err := <-closed:
						if err != nil || !a.hasPhysicalResources() {
							t.Fatal("parked close freed active state", err)
						}
					case <-time.After(5 * time.Second):
						t.Fatal("close blocked behind parked shared-home callback")
					}
				} else {
					cancel()
					deadline := time.Now().Add(5 * time.Second)
					for atomic.LoadUint32((*uint32)(unsafe.Pointer(&a.trap[0]))) != uint32(coreruntime.TrapInterrupted) && time.Now().Before(deadline) {
						runtime.Gosched()
					}
					if atomic.LoadUint32((*uint32)(unsafe.Pointer(&a.trap[0]))) != uint32(coreruntime.TrapInterrupted) {
						t.Fatal("outer cancellation not published after nested shared-home execution")
					}
				}
				unblock()
				select {
				case result := <-done:
					joined = true
					var trap *coreruntime.TrapError
					if result.values != nil || closeInstance && (!errors.As(result.err, &trap) || trap.Code != coreruntime.TrapInterrupted) || !closeInstance && !errors.Is(result.err, context.Canceled) {
						t.Fatal("shared-home host interruption", result)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("interrupted shared-home host did not unwind")
				}
				if memory.UnsafeBytes()[64] != 0 {
					t.Fatal("interrupted host resumed guest marker store")
				}
				importedGlobalInvoke(t, b, "private", []uint64{1, 2}, 3, 4)
				if closeInstance {
					if _, err := a.Invoke("host", args...); err == nil || a.hasPhysicalResources() {
						t.Fatal("closed shared-home instance remained callable or allocated", err)
					}
				} else if values, err := a.Invoke("host", args...); err != nil || len(values) != 16 || memory.UnsafeBytes()[64] != 9 {
					t.Fatal("shared-home instance failed post-cancel reuse", values, err)
				}
			})
		}
	}
}

func TestRV64PublicSWARStateHostCombinedImportBoundary(t *testing.T) {
	entries := make([][]byte, 0, 513)
	for range 256 {
		entries = append(entries, wasmtest.GlobalImportEntry("env", "v", wasm.V128, false))
	}
	entries = append(entries, append(append(wasmtest.Name("env"), wasmtest.Name("memory")...), 2, 1, 1, 4))
	for range 256 {
		entries = append(entries, append(append(wasmtest.Name("env"), wasmtest.Name("host")...), 0, 0))
	}
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType(nil, []wasm.ValType{wasm.V128, wasm.I32, wasm.V128}))),
		wasmtest.Section(2, wasmtest.Vec(entries...)), wasmtest.Section(3, []byte{1, 1}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("last", 0, 256))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 0xff, 1, 0x41, 7, 0x10, 0xff, 1, 0x41, 3, 0xfd, 0, 0, 0, 0x0b}))),
	)
	c, err := Compile(nil, source)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.NumImports != 256 || len(c.GlobalImports) != 256 || len(c.Funcs) != 1 {
		t.Fatal("maximum interleaved state/function metadata")
	}
	blob, err := c.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadTrustedArtifact(blob)
	if loaded != nil {
		defer loaded.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	_, memory := importedMemoryProvider(t)
	binary.LittleEndian.PutUint64(memory.UnsafeBytes()[3:], 0x7ff0000000000001)
	binary.LittleEndian.PutUint64(memory.UnsafeBytes()[11:], 0xff8000017f800001)
	for _, code := range []*Compiled{c, loaded} {
		imports := NewImports().Global("env", "v", GlobalImport{Type: ValV128, V128: rv64PublicVector(0x80000000deadbeef, 0x7ff0000000000042)}).Memory("env", "memory", memory)
		calls := 0
		imports.Function("env", "host", HostCallFunc(func(call HostCall) {
			if call.ParamCount() != 1 || call.ResultCount() != 1 || call.ParamSlots()[0] != 7 {
				panic("last scalar dispatch slots")
			}
			calls++
			call.ResultSlots()[0] = 0xdeadbeefffffffff
		}))
		in, err := Instantiate(code, imports)
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		importedGlobalInvoke(t, in, "last", []uint64{0x80000000deadbeef, 0x7ff0000000000042, 0xffffffff, 0x7ff0000000000001, 0xff8000017f800001})
		if err := in.Close(); err != nil || calls != 1 {
			t.Fatal("last dispatch row or close", calls, err)
		}
	}
}
