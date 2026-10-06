//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// The imported export is function ordinal1 despite four mixed import entries.
// The only defined function is narrow, so it cannot size wide relay buffers.
func rv64NativeReexportModule(types []wasm.ValType, shared, vector bool) []byte {
	imports := [][]byte{wasmtest.GlobalImportEntry("env", "flag", wasm.I32, true), rv64VectorFunctionImport("unused", 1)}
	if shared {
		imports = append(imports, rv64VectorMemoryImport())
	}
	imports = append(imports, rv64VectorFunctionImport("target", 0))
	body := []byte{0x20, 0}
	if vector {
		body = append(body, 0xfd, 12)
		body = append(body, make([]byte, 16)...)
		body = append(body, 0x1a)
	}
	body = append(body, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(types, types), wasmtest.FuncType(nil, nil), wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(imports...)), wasmtest.Section(3, []byte{1, 2}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("relay", 0, 1), wasmtest.ExportEntry("alias", 0, 1), wasmtest.ExportEntry("safe", 0, 2))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
}

func TestRV64PublicNativeReexportTransportRetirementAndFrozenAliases(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, shared := range []bool{false, true} {
			for _, shape := range []string{"void", "mixed4", "scalar64", "vector128"} {
				t.Run(fmt.Sprintf("artifact%t-shared%t-%s", artifact, shared, shape), func(t *testing.T) {
					count := 4
					if shape == "void" {
						count = 0
					}
					if shape == "scalar64" {
						count = 64
					}
					if shape == "vector128" {
						count = 128
					}
					types := make([]wasm.ValType, count)
					for i := range types {
						types[i] = []wasm.ValType{wasm.I32, wasm.F32, wasm.I64, wasm.F64}[i%4]
						if shape == "vector128" {
							types[i] = wasm.V128
						}
					}
					input, want := rv64NativeVectorInputs(types)
					flag, fail := NewGlobalI32(0, true), NewGlobalI32(0, true)
					defer flag.Close()
					defer fail.Close()
					var memory *Memory
					if shared {
						_, memory = importedMemoryProvider(t)
						defer memory.Close()
					}
					imports := NewImports().Global("env", "fail", fail)
					if shared {
						imports.Memory("env", "memory", memory)
					}
					producer, err := Instantiate(rv64PublicStartCompile(t, rv64NativeVectorProducer(types, shared), artifact), imports)
					if err != nil {
						t.Fatal(err)
					}
					defer producer.Close()
					target, err := producer.ExportedFunc("identity")
					if err != nil {
						t.Fatal(err)
					}
					c := rv64PublicStartCompile(t, rv64NativeReexportModule(types, shared, shape == "vector128"), artifact)
					// Public metadata is a detached view; entry uses the compiled snapshot.
					c.Exports["relay"] = 99999
					delete(c.Exports, "alias")
					imports = NewImports().Global("env", "flag", flag).Function("env", "unused", func() {}).Function("env", "target", target)
					if shared {
						imports.Memory("env", "memory", memory)
					}
					relay, err := Instantiate(c, imports)
					if err != nil {
						t.Fatal(err)
					}
					defer relay.Close()
					if len(relay.serArgs) < len(input)*8 || len(relay.results) < len(want)*8 {
						t.Fatal("reexport did not size its own transport")
					}
					for _, name := range []string{"relay", "alias"} {
						for range 3 {
							got, err := relay.Invoke(name, input...)
							if err != nil || !slices.Equal(got, want) {
								t.Fatal(name, got, err)
							}
						}
					}
					if _, err := relay.Invoke("relay", append(input, 9)...); err == nil {
						t.Fatal("wrong physical arity accepted")
					}
					ctx, cancel := context.WithCancel(context.Background())
					cancel()
					if _, err := relay.InvokeContext(ctx, "relay", input...); !errors.Is(err, context.Canceled) {
						t.Fatal("pre-cancellation", err)
					}
					if _, err := relay.WasmFunc("relay"); err == nil || !strings.Contains(err.Error(), "re-exported imports") {
						t.Fatal("prepared reexport policy", err)
					}
					reexport, err := relay.ExportedFunc("alias")
					if err != nil || reexport != target {
						t.Fatal("reexport target identity", err)
					}
					fail.Set(1)
					_, err = relay.Invoke("relay", input...)
					var trap *coreruntime.TrapError
					if !errors.As(err, &trap) || trap.Code != coreruntime.TrapUnreachable || len(trap.Frames) != 1 || trap.Frames[0].FunctionName != "identity" || trap.Frames[0].ProgramCounter != 5 {
						t.Fatal("producer trap attribution", err)
					}
					fail.Set(0)
					if err := producer.Close(); err != nil {
						t.Fatal(err)
					}
					if got, err := relay.Invoke("alias", input...); err != nil || !slices.Equal(got, want) {
						t.Fatal("retired producer reexport", got, err)
					}
					if other, err := Instantiate(c, imports); other != nil || err == nil {
						if other != nil {
							other.Close()
						}
						t.Fatal("new binding retained closed producer", err)
					}
					relay.Close()
					if producer.jm != nil || relay.thunkMem != nil {
						t.Fatal("reexport ownership leaked")
					}
				})
			}
		}
	}
}

func TestRV64PublicNativeReexportBindingAndOwnedEntryGates(t *testing.T) {
	types := []wasm.ValType{wasm.I32}
	flag := NewGlobalI32(0, true)
	defer flag.Close()
	for _, artifact := range []bool{false, true} {
		c := rv64PublicStartCompile(t, rv64NativeReexportModule(types, false, false), artifact)
		rt := NewRuntime()
		defer rt.Close()
		owner, err := rt.NewHostFuncRef(func(v int32) int32 { return v }, FuncSig{Params: []ValType{ValI32}, Results: []ValType{ValI32}})
		if err != nil {
			t.Fatal(err)
		}
		defer owner.Close()
		before := c.codeCache.base
		for _, binding := range []any{func(v int32) int32 { return v }, owner, (*InstanceExport)(nil)} {
			in, err := Instantiate(c, NewImports().Global("env", "flag", flag).Function("env", "unused", func() {}).Function("env", "target", binding))
			if in != nil || err == nil || c.codeCache.base != before {
				if in != nil {
					in.Close()
				}
				t.Fatal("non-native unused reexport published", err)
			}
		}
		producer, err := Instantiate(rv64PublicStartCompile(t, rv64PublicNativeIdentityModule(types), artifact))
		if err != nil {
			t.Fatal(err)
		}
		defer producer.Close()
		target, err := producer.ExportedFunc("identity")
		if err != nil {
			t.Fatal(err)
		}
		relay, err := Instantiate(c, NewImports().Global("env", "flag", flag).Function("env", "unused", func() {}).Function("env", "target", target))
		if err != nil {
			t.Fatal(err)
		}
		defer relay.Close()
		ctx := unsafe.Slice((*byte)(offHeapPtr(relay.nativeContext)), coreruntime.InstanceContextBytes)
		dispatch := uintptr(binary.LittleEndian.Uint64(ctx[coreruntime.InstanceContextImportDispatchOffset:]))
		row := unsafe.Slice((*byte)(offHeapPtr(dispatch)), 2*coreruntime.ImportDispatchEntryBytes)[coreruntime.ImportDispatchEntryBytes:]
		entry := binary.LittleEndian.Uint64(row)
		for _, bad := range []uint64{0, uint64(producer.base) + uint64(producer.c.Entry[0]), entry + 1} {
			binary.LittleEndian.PutUint64(row, bad)
			if _, err := relay.Invoke("relay", 9); err == nil {
				t.Fatal("unowned/unaligned reexport entry accepted")
			}
		}
		binary.LittleEndian.PutUint64(row, entry)
		if got, err := relay.Invoke("relay", 9); err != nil || !slices.Equal(got, []uint64{9}) {
			t.Fatal("entry rejection damaged relay", got, err)
		}
		relay.Close()
		producer.Close()
		// A scalar same-cell-width but different logical signature must fail early.
		wrong, err := Instantiate(rv64PublicStartCompile(t, rv64PublicNativeIdentityModule([]wasm.ValType{wasm.F32}), artifact))
		if err != nil {
			t.Fatal(err)
		}
		wrongTarget, err := wrong.ExportedFunc("identity")
		if err != nil {
			t.Fatal(err)
		}
		in, err := Instantiate(c, NewImports().Global("env", "flag", flag).Function("env", "unused", func() {}).Function("env", "target", wrongTarget))
		wrong.Close()
		if in != nil || err == nil {
			if in != nil {
				in.Close()
			}
			t.Fatal("logical signature mismatch accepted", err)
		}
	}
}

func TestRV64PublicNativeReexportConcurrentParkedRelaysAndProducer(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, shared := range []bool{false, true} {
			t.Run(fmt.Sprintf("artifact%t-shared%t", artifact, shared), func(t *testing.T) {
				var memory *Memory
				if shared {
					_, memory = importedMemoryProvider(t)
					defer memory.Close()
				}
				flag := NewGlobalI32(0, true)
				defer flag.Close()
				var owners [3]*Instance
				entered := make(chan int, 3)
				producerAttempt := make(chan int, 2)
				var release [3]chan struct{}
				var once [3]sync.Once
				for i := range release {
					release[i] = make(chan struct{})
				}
				unblock := func(i int) { once[i].Do(func() { close(release[i]) }) }
				defer func() {
					for i := range release {
						unblock(i)
					}
				}()
				host := CallerHostCallFunc(func(caller Caller, call HostCall) {
					i := int(call.I32(0)) - 1
					if i < 0 || i >= 3 {
						panic("call carrier corrupted")
					}
					// Successful relay reentry proves the host activation carries its root's identity.
					got, err := owners[i].InvokeFromHost(context.Background(), caller, "safe", 41)
					if err != nil || !slices.Equal(got, []uint64{41}) {
						panic(fmt.Sprintf("root host reentry: %v %v", got, err))
					}
					runtime.GC()
					entered <- i
					<-release[i]
					if call.I32(0) != int32(i+1) {
						panic("borrowed host argument changed while parked")
					}
					if i < 2 {
						producerAttempt <- i
						ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
						defer cancel()
						got, err := owners[2].InvokeFromHost(ctx, caller, "safe", 55)
						if err != nil || !slices.Equal(got, []uint64{55}) {
							panic(fmt.Sprintf("serialized producer entry: %v %v", got, err))
						}
					}
					call.SetI32(0, int32(100+i))
				})
				imports := testImports("env.host", host)
				if shared {
					imports.Memory("env", "memory", memory)
				}
				producer, err := Instantiate(rv64PublicStartCompile(t, rv64PublicNativeHostModule(shared, "producer", nil), artifact), imports)
				if err != nil {
					t.Fatal(err)
				}
				defer producer.Close()
				owners[2] = producer
				target, err := producer.ExportedFunc("producer")
				if err != nil {
					t.Fatal(err)
				}
				c := rv64PublicStartCompile(t, rv64NativeReexportModule([]wasm.ValType{wasm.I32}, shared, false), artifact)
				for i := 0; i < 2; i++ {
					imports := NewImports().Global("env", "flag", flag).Function("env", "unused", func() {}).Function("env", "target", target)
					if shared {
						imports.Memory("env", "memory", memory)
					}
					owners[i], err = Instantiate(c, imports)
					if err != nil {
						t.Fatal(err)
					}
					defer owners[i].Close()
				}
				type result struct {
					value []uint64
					err   error
				}
				var done [3]chan result
				var wg sync.WaitGroup
				for i := range done {
					done[i] = make(chan result, 1)
					wg.Add(1)
					go func(i int) {
						defer wg.Done()
						name := "relay"
						if i == 2 {
							name = "producer"
						}
						got, err := owners[i].Invoke(name, uint64(i+1))
						done[i] <- result{slices.Clone(got), err}
					}(i)
				}
				defer func() {
					for i := range release {
						unblock(i)
					}
					wg.Wait()
				}()
				var seen [3]bool
				for range 3 {
					select {
					case i := <-entered:
						if seen[i] {
							t.Fatal("duplicate activation")
						}
						seen[i] = true
					case <-time.After(5 * time.Second):
						t.Fatal("relays/producer did not park independently")
					}
				}
				unblock(0)
				unblock(1)
				for range 2 {
					select {
					case <-producerAttempt:
					case <-time.After(5 * time.Second):
						t.Fatal("producer entries did not queue")
					}
				}
				// Producer owns its gate while parked. Both foreign callbacks must
				// wait rather than swapping its canonical scratch concurrently.
				for _, i := range []int{2, 1, 0} {
					unblock(i)
					select {
					case got := <-done[i]:
						if got.err != nil || !slices.Equal(got.value, []uint64{uint64(100 + i)}) {
							t.Fatalf("independent parked transport %d: values=%v error=%v", i, got.value, got.err)
						}
					case <-time.After(5 * time.Second):
						t.Fatal("parked relay did not finish")
					}
				}
			})
		}
	}
}

func TestRV64PublicNativeReexportCancellationAndCloseWhileProducerParked(t *testing.T) {
	for _, closeRoot := range []bool{false, true} {
		t.Run(fmt.Sprintf("close%t", closeRoot), func(t *testing.T) {
			// An independently retained cell witnesses every incorrect guest resume,
			// including after the producer and consumer have physically closed.
			flag := NewGlobalI32(0, true)
			defer flag.Close()
			source := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
				wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "flag", wasm.I32, true), append(append(wasmtest.Name("env"), wasmtest.Name("host")...), 0, 0))),
				wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("producer", 0, 1))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x10, 0, 0x24, 0, 0x23, 0, 0x0b}))),
			)
			producerCode := rv64PublicStartCompile(t, source, true)
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			defer unblock()
			producerImports := testImports("env.host", func(v int32) int32 { close(entered); <-release; return v + 1 }).Global("env", "flag", flag)
			producer, err := Instantiate(producerCode, producerImports)
			if err != nil {
				t.Fatal(err)
			}
			defer producer.Close()
			export, err := producer.ExportedFunc("producer")
			if err != nil {
				t.Fatal(err)
			}
			consumer, err := Instantiate(rv64PublicStartCompile(t, rv64NativeReexportModule([]wasm.ValType{wasm.I32}, false, false), true), NewImports().Global("env", "flag", flag).Function("env", "unused", func() {}).Function("env", "target", export))
			if err != nil {
				t.Fatal(err)
			}
			defer consumer.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			finished := make(chan struct{})
			go func() { defer close(finished); _, err := consumer.InvokeContext(ctx, "relay", 7); done <- err }()
			defer func() {
				cancel()
				unblock()
				select {
				case <-finished:
				case <-time.After(5 * time.Second):
					t.Error("foreign activation cleanup did not finish")
					<-finished
				}
			}()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("foreign host did not park")
			}
			if err := producer.Close(); err != nil {
				t.Fatal(err)
			}
			if producer.jm == nil || len(producer.ctrl) == 0 || len(consumer.thunkMem) == 0 {
				t.Fatal("parked retained producer released")
			}
			if closeRoot {
				if err := consumer.Close(); err != nil {
					t.Fatal(err)
				}
			} else {
				cancel()
			}
			deadline := time.Now().Add(5 * time.Second)
			for atomic.LoadUint32((*uint32)(unsafe.Pointer(&consumer.trap[0]))) != uint32(coreruntime.TrapInterrupted) && time.Now().Before(deadline) {
				runtime.Gosched()
			}
			if atomic.LoadUint32((*uint32)(unsafe.Pointer(&consumer.trap[0]))) != uint32(coreruntime.TrapInterrupted) {
				t.Fatal("foreign watcher did not interrupt")
			}
			unblock()
			select {
			case err := <-done:
				var trap *coreruntime.TrapError
				if closeRoot {
					if !errors.As(err, &trap) || trap.Code != coreruntime.TrapInterrupted {
						t.Fatal("foreign Close unwind", err)
					}
				} else if !errors.Is(err, context.Canceled) {
					t.Fatal("foreign cancellation", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("foreign unwind did not finish")
			}
			if closeRoot && (producer.jm != nil || consumer.thunkMem != nil) {
				t.Fatal("foreign physical ownership leaked after unwind")
			}
			if flag.Get() != 0 {
				t.Fatal("interrupted foreign guest resumed its post-host store")
			}
		})
	}
}

func TestRV64PublicNativeReexportSameEdgeNestedWideTransport(t *testing.T) {
	for _, importOnly := range []bool{false, true} {
		for _, artifact := range []bool{false, true} {
			for _, shared := range []bool{false, true} {
				t.Run(fmt.Sprintf("importonly%t-artifact%t-shared%t", importOnly, artifact, shared), func(t *testing.T) {
					types := make([]wasm.ValType, 128)
					for i := range types {
						types[i] = wasm.V128
					}
					input, want := rv64NativeVectorInputs(types)
					nested := slices.Clone(input)
					for i := range nested {
						nested[i] ^= 0xa55a123456780000
					}
					imports := [][]byte{rv64VectorFunctionImport("host", 1)}
					var memory *Memory
					if shared {
						_, memory = importedMemoryProvider(t)
						defer memory.Close()
						imports = append(imports, rv64VectorMemoryImport())
					}
					body := []byte{0x10, 0}
					for i := range types {
						body = append(body, 0x20)
						body = append(body, wasmtest.ULEB(uint32(i))...)
					}
					body = append(body, 0x0b)
					source := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(types, types), wasmtest.FuncType(nil, nil))), wasmtest.Section(2, wasmtest.Vec(imports...)), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("identity", 0, 1))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
					var relay *Instance
					depth, calls := 0, 0
					var retained Caller
					host := CallerHostCallFunc(func(caller Caller, _ HostCall) {
						depth++
						defer func() { depth-- }()
						calls++
						runtime.GC()
						if depth == 1 {
							retained = caller
							got, err := relay.InvokeFromHost(context.Background(), caller, "relay", nested...)
							if err != nil || !slices.Equal(got, nested) {
								panic(fmt.Sprintf("same-edge nested vector cells: %v", err))
							}
							if h, ok := resolveHostCaller(caller); !ok || !h.valid() {
								panic("outer callback authority expired")
							}
						}
					})
					producerImports := testImports("env.host", host)
					if shared {
						producerImports.Memory("env", "memory", memory)
					}
					producer, err := Instantiate(rv64PublicStartCompile(t, source, artifact), producerImports)
					if err != nil {
						t.Fatal(err)
					}
					defer producer.Close()
					target, err := producer.ExportedFunc("identity")
					if err != nil {
						t.Fatal(err)
					}
					flag := NewGlobalI32(0, true)
					defer flag.Close()
					relayImports := NewImports().Global("env", "flag", flag).Function("env", "unused", func() {}).Function("env", "target", target)
					if shared {
						relayImports.Memory("env", "memory", memory)
					}
					relaySource := rv64NativeReexportModule(types, shared, true)
					if importOnly {
						relaySource = rv64PublicImportOnlyModule(types, shared, false, true)
					}
					relay, err = Instantiate(rv64PublicStartCompile(t, relaySource, artifact), relayImports)
					if err != nil {
						t.Fatal(err)
					}
					defer relay.Close()
					for range 3 {
						got, err := relay.Invoke("relay", input...)
						if err != nil || !slices.Equal(got, want) {
							t.Fatal("outer full256 cells lost across same-edge nested entry", got, err)
						}
					}
					if calls != 6 || depth != 0 {
						t.Fatal("nested host callback count", calls, depth)
					}
					if _, err := relay.InvokeFromHost(context.Background(), retained, "safe", 4); !errors.Is(err, ErrPermissionDenied) {
						t.Fatal("retained callback authority survived", err)
					}
					identity := producer.nativeContext
					producer.Close()
					relay.Close()
					rv64NativeHostOwners.Lock()
					_, left := rv64NativeHostOwners.values[identity]
					rv64NativeHostOwners.Unlock()
					if left {
						t.Fatal("native host owner registration survived physical close")
					}
				})
			}
		}
	}
}
