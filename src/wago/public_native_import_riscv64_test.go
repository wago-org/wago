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

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64PublicNativeIdentityModule(params []wasm.ValType) []byte {
	body := []byte{}
	for index := range params {
		body = append(body, 0x20)
		body = append(body, wasmtest.ULEB(uint32(index))...)
	}
	body = append(body, 0x0b)
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, params))), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("identity", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
}

func TestRV64PublicNativeImportTypedTransportOwnershipAndSessions(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, arity := range []int{0, 4, 64} {
			t.Run(fmt.Sprintf("artifact%t-arity%d", artifact, arity), func(t *testing.T) {
				params := make([]wasm.ValType, arity)
				input, want := make([]uint64, arity), make([]uint64, arity)
				for index := range params {
					params[index] = []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64}[index%4]
					input[index] = []uint64{0xfeedface80000001, 0xffedcba987654321, 0xdeadbeef7f800123, 0x7ff0000000000001}[index%4] + uint64(index)
					want[index] = input[index]
					if index%4 == 0 || index%4 == 2 {
						want[index] = uint64(uint32(want[index]))
					}
				}
				producerCode := rv64PublicStartCompile(t, rv64PublicNativeIdentityModule(params), artifact)
				producer, err := Instantiate(producerCode)
				if err != nil {
					t.Fatal(err)
				}
				defer producer.Close()
				export, err := producer.ExportedFunc("identity")
				if err != nil {
					t.Fatal(err)
				}
				consumerCode := rv64PublicStartCompile(t, rv64PublicHostModule(params, params), artifact)
				before := producer.referenceLifetime().snapshot().ResourceRoots
				first, err := Instantiate(consumerCode, testImports("env.host", export))
				if err != nil {
					t.Fatal(err)
				}
				defer first.Close()
				second, err := Instantiate(consumerCode, testImports("env.host", export))
				if err != nil {
					t.Fatal(err)
				}
				defer second.Close()
				if first.base != second.base || len(first.thunkMem) == 0 || len(second.thunkMem) == 0 || offHeapSlicePtr(first.thunkMem) == offHeapSlicePtr(second.thunkMem) || first.syncMode || producer.referenceLifetime().snapshot().ResourceRoots != before+2 {
					t.Fatal("native bridge ownership/publication")
				}
				for _, in := range []*Instance{first, second, producer} {
					name := "call"
					if in == producer {
						name = "identity"
					}
					got, err := in.Invoke(name, input...)
					if err != nil || !slices.Equal(got, want) {
						t.Fatal("typed public native import", got, err)
					}
				}
				fn, err := first.WasmFunc("call")
				if err != nil {
					t.Fatal(err)
				}
				if got, err := fn.Invoke(input...); err != nil || !slices.Equal(got, want) {
					t.Fatal("prepared import", got, err)
				}
				if arity == 4 {
					session, err := fn.OpenSession()
					if err != nil {
						t.Fatal(err)
					}
					got, err := session.Invoke4(input[0], input[1], input[2], input[3])
					session.Close()
					if err != nil || !slices.Equal(got, want) {
						t.Fatal("native session", got, err)
					}
				}
				if allocs := testing.AllocsPerRun(20, func() {
					got, err := first.Invoke("call", input...)
					if err != nil || !slices.Equal(got, want) {
						panic("warm native import")
					}
				}); allocs != 0 {
					t.Fatal("warm native allocations", allocs)
				}
				if err := producer.Close(); err != nil {
					t.Fatal(err)
				}
				if producer.jm == nil || producer.nativeContext == 0 {
					t.Fatal("retired producer freed despite attached consumers")
				}
				if err := producerCode.Close(); err != nil {
					t.Fatal(err)
				}
				if err := consumerCode.Close(); err != nil {
					t.Fatal(err)
				}
				if got, err := first.Invoke("call", input...); err != nil || !slices.Equal(got, want) {
					t.Fatal("retired attached producer", got, err)
				}
				if err := first.Close(); err != nil {
					t.Fatal(err)
				}
				if producer.jm == nil || first.thunkMem != nil {
					t.Fatal("first close lost sibling retention")
				}
				if got, err := second.Invoke("call", input...); err != nil || !slices.Equal(got, want) {
					t.Fatal("remaining edge", got, err)
				}
				if err := second.Close(); err != nil {
					t.Fatal(err)
				}
				if producer.jm != nil || producer.nativeContext != 0 || second.thunkMem != nil {
					t.Fatal("last edge leaked producer/mapping")
				}
			})
		}
	}
}

func TestRV64PublicNativeImportNarrowExportWideInternalTransport(t *testing.T) {
	wide := make([]wasm.ValType, 128)
	for i := range wide {
		wide[i] = wasm.V128
	}
	identity, narrow := []byte{}, []byte{}
	for i := range wide {
		identity = append(identity, 0x20)
		identity = append(identity, wasmtest.ULEB(uint32(i))...)
		narrow = append(narrow, 0xfd, 12)
		narrow = append(narrow, make([]byte, 16)...)
	}
	identity = append(identity, 0x0b)
	narrow = append(narrow, 0x10, 0)
	for range wide {
		narrow = append(narrow, 0x1a)
	}
	narrow = append(narrow, 0x41, 17, 0x0b)
	source := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(wide, wide), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))), wasmtest.Section(3, []byte{2, 0, 1}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("narrow", 0, 1))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(identity), wasmtest.Code(narrow))))
	for _, artifact := range []bool{false, true} {
		producer, err := Instantiate(rv64PublicStartCompile(t, source, artifact))
		if err != nil {
			t.Fatal(err)
		}
		defer producer.Close()
		export, err := producer.ExportedFunc("narrow")
		if err != nil {
			t.Fatal(err)
		}
		consumer, err := Instantiate(rv64PublicStartCompile(t, rv64PublicHostModule(nil, []wasm.ValType{wasm.I32}), artifact), testImports("env.host", export))
		if err != nil {
			t.Fatal(err)
		}
		defer consumer.Close()
		for range 3 {
			if got, err := consumer.Invoke("call"); err != nil || !slices.Equal(got, []uint64{17}) {
				t.Fatal("wide internal edge transport", got, err)
			}
		}
	}
}

// One import and two locals. Memory, when present, precedes the function import
// in the section while the native call index remains zero.
func rv64PublicNativeHostModule(sharedMemory bool, name string, tail []byte) []byte {
	imports := [][]byte{append(append(wasmtest.Name("env"), wasmtest.Name("host")...), 0, 0)}
	if sharedMemory {
		imports = append([][]byte{append(append(wasmtest.Name("env"), wasmtest.Name("memory")...), 2, 1, 1, 4)}, imports...)
	}
	body := append([]byte{0x20, 0, 0x10, 0}, tail...)
	body = append(body, 0x0b)
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))), wasmtest.Section(2, wasmtest.Vec(imports...)), wasmtest.Section(3, []byte{2, 0, 0}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry(name, 0, 1), wasmtest.ExportEntry("safe", 0, 2))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body), wasmtest.Code([]byte{0x20, 0, 0x0b}))))
}

func TestRV64PublicNativeImportHostReentryProducerTrapsAndSharedHomes(t *testing.T) {
	for _, shared := range []bool{false, true} {
		for _, artifact := range []bool{false, true} {
			t.Run(fmt.Sprintf("shared%t-artifact%t", shared, artifact), func(t *testing.T) {
				var memory *Memory
				if shared {
					_, memory = importedMemoryProvider(t)
				}
				consumerCode := rv64PublicStartCompile(t, rv64PublicNativeHostModule(shared, "consumer", []byte{0x20, 0, 0x41, 2, 0x46, 0x04, 0x40, 0x00, 0x0b}), artifact)
				// The producer traps after its Go callback when argument zero is passed.
				producerCode := rv64PublicStartCompile(t, rv64PublicNativeHostModule(shared, "producer", []byte{0x20, 0, 0x45, 0x04, 0x40, 0x00, 0x0b}), artifact)
				var consumer, producer *Instance
				calls := 0
				host := CallerHostCallFunc(func(caller Caller, call HostCall) {
					calls++
					if got, err := consumer.InvokeFromHost(context.Background(), caller, "safe", 49); err != nil || !slices.Equal(got, []uint64{49}) {
						panic("nested caller entry")
					}
					if shared && len(caller.Memory()) != 65536 {
						panic("foreign host memory")
					}
					runtime.GC()
					call.SetI32(0, call.I32(0)+1)
				})
				imports := testImports("env.host", host)
				if shared {
					imports.Memory("env", "memory", memory)
				}
				var err error
				producer, err = Instantiate(producerCode, imports)
				if err != nil {
					t.Fatal(err)
				}
				defer producer.Close()
				export, err := producer.ExportedFunc("producer")
				if err != nil {
					t.Fatal(err)
				}
				imports = testImports("env.host", export)
				if shared {
					imports.Memory("env", "memory", memory)
				}
				consumer, err = Instantiate(consumerCode, imports)
				if err != nil {
					t.Fatal(err)
				}
				defer consumer.Close()
				if shared && (producer.jm != consumer.jm || producer.nativeContext == consumer.nativeContext) {
					t.Fatal("shared-home context fixture")
				}
				for range 3 {
					if got, err := consumer.Invoke("consumer", 7); err != nil || !slices.Equal(got, []uint64{8}) {
						t.Fatal("foreign host normal return", got, err)
					}
					_, err := consumer.Invoke("consumer", 0)
					var trap *coreruntime.TrapError
					if !errors.As(err, &trap) || trap.Code != coreruntime.TrapUnreachable || len(trap.Frames) != 1 || trap.Frames[0].FunctionIndex != 1 || trap.Frames[0].FunctionName != "producer" || !trap.Frames[0].HasProgramCounter {
						t.Fatal("producer attribution", err)
					}
					_, err = consumer.Invoke("consumer", 2)
					if !errors.As(err, &trap) || trap.Code != coreruntime.TrapUnreachable || len(trap.Frames) != 1 || trap.Frames[0].FunctionName != "consumer" {
						t.Fatal("caller source after producer return", err)
					}
					if got, err := consumer.Invoke("safe", 51); err != nil || !slices.Equal(got, []uint64{51}) {
						t.Fatal("caller cold-unwind reuse", got, err)
					}
					if got, err := producer.Invoke("safe", 52); err != nil || !slices.Equal(got, []uint64{52}) {
						t.Fatal("producer cold-unwind reuse", got, err)
					}
				}
				if calls != 9 {
					t.Fatal("foreign callback count", calls)
				}
				if err := producer.Close(); err != nil {
					t.Fatal(err)
				}
				_, err = consumer.Invoke("consumer", 0)
				var trap *coreruntime.TrapError
				if !errors.As(err, &trap) || len(trap.Frames) != 1 || trap.Frames[0].FunctionName != "producer" {
					t.Fatal("retired producer source", err)
				}
			})
		}
	}
}

func TestRV64PublicNativeImportCancellationAndCloseWhileProducerParked(t *testing.T) {
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
			consumer, err := Instantiate(rv64PublicStartCompile(t, rv64PublicHostModule([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), true), testImports("env.host", export))
			if err != nil {
				t.Fatal(err)
			}
			defer consumer.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			finished := make(chan struct{})
			go func() { defer close(finished); _, err := consumer.InvokeContext(ctx, "call", 7); done <- err }()
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

func TestRV64PublicNativeImportAdmissionABIAndArenaBeforePublication(t *testing.T) {
	producerCode := rv64PublicStartCompile(t, rv64PublicNativeIdentityModule([]wasm.ValType{wasm.I32}), false)
	producer, err := Instantiate(producerCode)
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	export, err := producer.ExportedFunc("identity")
	if err != nil {
		t.Fatal(err)
	}
	consumerSource := rv64PublicHostModule([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	for _, artifact := range []bool{false, true} {
		c := rv64PublicStartCompile(t, consumerSource, artifact)
		// Frozen ABI metadata is authoritative for both consumer and producer.
		beforeBase, beforeSealed, beforeRefs := c.codeCache.base, c.codeCache.sealed, c.codeCache.refs
		beforeRoots := producer.referenceLifetime().snapshot().ResourceRoots
		producer.c.nativeContextTrapABI = false
		in, err := Instantiate(c, testImports("env.host", export))
		producer.c.nativeContextTrapABI = true
		if in != nil || !errors.Is(err, railshot.ErrUnsupported) || c.codeCache.base != beforeBase || c.codeCache.sealed != beforeSealed || c.codeCache.refs != beforeRefs || producer.referenceLifetime().snapshot().ResourceRoots != beforeRoots {
			t.Fatal("originless producer published", err)
		}
		bad := &InstanceExport{inst: producer, localIdx: 99}
		if in, err := Instantiate(c, testImports("env.host", bad)); in != nil || err == nil || c.codeCache.sealed != beforeSealed {
			t.Fatal("invalid producer entry published", err)
		}
		// An originless old experimental artifact is deliberately incompatible.
		view := rv64ReferenceImportTailCopy(c)
		view.nativeContextTrapABI = false
		view.nativeTableContextABI = false
		view.nativeTableReferenceABI = false
		if err := railshotValidateCompiled(view); !errors.Is(err, railshot.ErrUnsupported) {
			t.Fatal("legacy origin ABI admitted", err)
		}
		rv64RequireRejectedArtifact(t, view)
	}
	consumer := rv64PublicStartCompile(t, consumerSource, false)
	if err := consumer.executionView().validateCached(); err != nil {
		t.Fatal(err)
	}
	resolved := testImports("env.host", export).bindings
	need := consumer.executionView().arenaNeedForImports(resolved, consumer.executionView().importsRequireSync(resolved, false))
	if railshotNativeImportArenaBytes(consumer.executionView(), resolved) != 0 {
		t.Fatal("stack transport acquired unused arena storage")
	}
	rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithMaxInstanceMetadataBytes(uint64(need - 1))))
	defer rt.Close()
	mod, err := rt.Module(consumer)
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()
	before := producer.referenceLifetime().snapshot().ResourceRoots
	if in, err := rt.Instantiate(context.Background(), mod, WithImports(testImports("env.host", export))); in != nil || err == nil || producer.referenceLifetime().snapshot().ResourceRoots != before {
		t.Fatal("native edge quota/retention", err)
	}
	if err := producer.Close(); err != nil {
		t.Fatal(err)
	}
	fresh := rv64PublicStartCompile(t, consumerSource, false)
	if in, err := Instantiate(fresh, testImports("env.host", export)); in != nil || err == nil || fresh.codeCache.base != 0 {
		t.Fatal("new binding to closed producer published", err)
	}
}

func TestRV64PublicNativeImportTrapGraphBoundAndRethrow(t *testing.T) {
	// Exercise resolver storage and edge budgets without allocating machine code.
	root := &Instance{c: &Compiled{Exports: map[string]int{"root": 0}}, nativeContext: 1}
	owners := make([]*Instance, 256)
	root.c.Imports = make([]string, 256)
	root.imports = make(resolvedImports)
	for i := range owners {
		owners[i] = &Instance{c: &Compiled{Exports: map[string]int{fmt.Sprintf("producer%d", i): 0}}, nativeContext: uintptr(i + 2)}
		root.c.Imports[i] = fmt.Sprintf("env.%d", i)
		root.imports[root.c.functionImportBindingKey(i)] = &InstanceExport{inst: owners[i]}
	}
	// Source provenance is captured through the real Engine parser, then consumed
	// by the public decorator. The raw identity is never pointer-dereferenced.
	code := rv64PublicStartCompile(t, wasmtest.Module(wasmtest.Section(1, []byte{1, 0x60, 0, 0}), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("trap", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0, 0x0b})))), false)
	fixture, err := Instantiate(code)
	if err != nil {
		t.Fatal(err)
	}
	defer fixture.Close()
	jm, engine, trapBytes := fixture.jm, fixture.eng, fixture.trap
	if err := jm.BindTrapCell(trapBytes); err != nil {
		t.Fatal(err)
	}
	jm.SetStackFence(engine.StackLimit())
	for _, identity := range []uintptr{owners[255].nativeContext, 0xabcdef, 0} {
		jm.SetRV64NativeContext(identity)
		err := engine.CallPrepared(fixture.base+uintptr(fixture.c.Entry[0]), nil, jm.LinMemBase(), trapBytes, nil)
		var trap *coreruntime.TrapError
		if !errors.As(err, &trap) || len(trap.Frames) != 1 {
			t.Fatal("graph provenance fixture", err)
		}
		root.decorateTrap(err)
		want := ""
		if identity == owners[255].nativeContext {
			want = "producer255"
		}
		if trap.Frames[0].FunctionName != want {
			t.Fatal("graph owner", identity, trap.Frames)
		}
		trap.Frames[0].FunctionName = ""
		root.nativeContext = identity
		root.decorateTrap(err)
		if trap.Frames[0].FunctionName != "" {
			t.Fatal("rethrow acquired recycled context owner")
		}
		root.nativeContext = 1
	}
	// A cycle is visited once; an unknown origin terminates within the edge bound.
	owners[0].c.Imports = []string{"env.cycle"}
	owners[0].imports = resolvedImports{owners[0].c.functionImportBindingKey(0): &InstanceExport{inst: root}}
	jm.SetRV64NativeContext(0xfedcba)
	err = engine.CallPrepared(fixture.base+uintptr(fixture.c.Entry[0]), nil, jm.LinMemBase(), trapBytes, nil)
	if owner, pending := railshotTrapOwner(root, err.(*coreruntime.TrapError)); owner != nil || !pending {
		t.Fatal("cycle resolved unknown origin")
	}
	if binary.LittleEndian.Uint64(trapBytes[8:16]) != 0xfedcba {
		t.Fatal("cold trap origin payload")
	}
	// Dense frozen graphs stop at the explicit edge budget. The origin beyond
	// the scanned budget stays unnamed, even though its owner is attached.
	for _, owner := range owners {
		owner.c.Imports = make([]string, 256)
		owner.imports = make(resolvedImports)
		for index := range owner.c.Imports {
			owner.c.Imports[index] = fmt.Sprintf("env.edge%d", index)
			owner.imports[owner.c.functionImportBindingKey(index)] = &InstanceExport{inst: root}
		}
	}
	beyond := &Instance{c: &Compiled{Exports: map[string]int{"beyond": 0}}, nativeContext: 99999}
	owners[15].imports[owners[15].c.functionImportBindingKey(255)] = &InstanceExport{inst: beyond}
	jm.SetRV64NativeContext(beyond.nativeContext)
	err = engine.CallPrepared(fixture.base+uintptr(fixture.c.Entry[0]), nil, jm.LinMemBase(), trapBytes, nil)
	root.decorateTrap(err)
	if err.(*coreruntime.TrapError).Frames[0].FunctionName != "" {
		t.Fatal("edge exhaustion attributed beyond scan budget")
	}
}

func TestRV64PublicNativeImportMixedOwnedHostBlobsAndRuntime(t *testing.T) {
	for _, nativeFirst := range []bool{false, true} {
		for _, artifact := range []bool{false, true} {
			t.Run(fmt.Sprintf("nativefirst%t-artifact%t", nativeFirst, artifact), func(t *testing.T) {
				rt := NewRuntime()
				defer rt.Close()
				producer, err := Instantiate(rv64PublicStartCompile(t, rv64PublicNativeIdentityModule([]wasm.ValType{wasm.I32}), artifact))
				if err != nil {
					t.Fatal(err)
				}
				defer producer.Close()
				export, err := producer.ExportedFunc("identity")
				if err != nil {
					t.Fatal(err)
				}
				owner, err := rt.NewHostFuncRef(func(v int32) int32 { return v + 1 }, FuncSig{Params: []ValType{ValI32}, Results: []ValType{ValI32}})
				if err != nil {
					t.Fatal(err)
				}
				defer owner.Close()
				source := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))), wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("env"), wasmtest.Name("first")...), 0, 0), append(append(wasmtest.Name("env"), wasmtest.Name("second")...), 0, 0))), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 2))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x10, 0, 0x10, 1, 0x0b}))))
				c := rv64PublicStartCompile(t, source, artifact)
				mod, err := rt.Module(c)
				if err != nil {
					t.Fatal(err)
				}
				defer mod.Close()
				var first, second any = owner, export
				if nativeFirst {
					first, second = export, owner
				}
				imports := testImports("env.first", first, "env.second", second)
				in, err := rt.Instantiate(context.Background(), mod, WithImports(imports))
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				if len(in.thunkMem) == 0 || !in.syncMode {
					t.Fatal("mixed private blob not owned")
				}
				dispatch := unsafe.Slice((*byte)(offHeapPtr(in.jm.CaptureInstanceContext().ImportDispatch)), 2*coreruntime.ImportDispatchEntryBytes)
				for index := 0; index < 2; index++ {
					if address := binary.LittleEndian.Uint64(dispatch[index*coreruntime.ImportDispatchEntryBytes:]); address == 0 || address&15 != 0 {
						t.Fatal("mixed RV64 entry alignment", address)
					}
				}
				if err := owner.Close(); err == nil {
					t.Fatal("host owner closed despite its attached importer")
				}
				if err := producer.Close(); err != nil {
					t.Fatal(err)
				}
				for range 3 {
					if got, err := in.Invoke("call", 0xdeadbeef80000001); err != nil || !slices.Equal(got, []uint64{0x80000002}) {
						t.Fatal("mixed retired producer and attached host owner", got, err)
					}
				}
			})
		}
	}
}

func TestRV64PublicNativeImportPublicationFailureRestoresSharedHomeAndRoots(t *testing.T) {
	_, memory := importedMemoryProvider(t)
	producerCode := rv64PublicStartCompile(t, rv64PublicNativeHostModule(true, "producer", nil), false)
	producer, err := Instantiate(producerCode, testImports("env.host", func(v int32) int32 { return v }).Memory("env", "memory", memory))
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	if got, err := producer.Invoke("safe", 19); err != nil || !slices.Equal(got, []uint64{19}) {
		t.Fatal("rollback prior owner", got, err)
	}
	export, err := producer.ExportedFunc("producer")
	if err != nil {
		t.Fatal(err)
	}
	c := rv64PublicStartCompile(t, rv64PublicNativeHostModule(true, "consumer", nil), false)
	indexes := c.ensureCompileIndexes()
	failure := errors.New("injected native thunk publication failure")
	publications := 0
	var retained []byte
	indexes.mapThunkCode = func([]byte) ([]byte, uintptr, error) {
		publications++
		retained = cleanupMapping(t)
		return retained, 0, failure
	}
	indexes.unmapCode = func(mem []byte) error {
		if offHeapSlicePtr(mem) == offHeapSlicePtr(retained) {
			return failure
		}
		return coreruntime.Unmap(mem)
	}
	defer func() { indexes.mapThunkCode, indexes.unmapCode = nil, nil }()
	beforeCtx, beforeOrigin := producer.jm.CaptureInstanceContext(), producer.jm.RV64NativeContext()
	beforeRoots := producer.referenceLifetime().snapshot().ResourceRoots
	imports := testImports("env.host", export).Memory("env", "memory", memory)
	in, err := Instantiate(c, imports)
	if in != nil || !errors.Is(err, failure) || publications != 1 || len(indexes.failedMappings) != 1 || producer.referenceLifetime().snapshot().ResourceRoots != beforeRoots || producer.jm.CaptureInstanceContext() != beforeCtx || producer.jm.RV64NativeContext() != beforeOrigin {
		t.Fatal("failed native publication lost rollback owner/context", err)
	}
	if in, err := Instantiate(c, imports); in != nil || err == nil || publications != 1 {
		t.Fatal("pending native cleanup permitted new publication", err)
	}
	indexes.mapThunkCode, indexes.unmapCode = nil, nil
	if err := c.Close(); err != nil || len(indexes.failedMappings) != 0 {
		t.Fatal("native rollback cleanup retry", err)
	}
	if got, err := producer.Invoke("safe", 23); err != nil || !slices.Equal(got, []uint64{23}) {
		t.Fatal("producer reuse after publication rollback", got, err)
	}
}
