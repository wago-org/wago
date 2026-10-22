//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
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

func rv64VectorFunctionImport(name string, typ uint32) []byte {
	entry := append(append(wasmtest.Name("env"), wasmtest.Name(name)...), 0)
	return append(entry, wasmtest.ULEB(typ)...)
}

func rv64VectorMemoryImport() []byte {
	return append(append(wasmtest.Name("env"), wasmtest.Name("memory")...), 2, 1, 1, 4)
}

func rv64NativeVectorProducer(params []wasm.ValType, shared bool) []byte {
	imports := [][]byte{wasmtest.GlobalImportEntry("env", "fail", wasm.I32, true)}
	if shared {
		imports = append(imports, rv64VectorMemoryImport())
	}
	// The producer's exact first-function cold PC is5 (including local decl0).
	body := []byte{0x23, 0, 0x04, 0x40, 0x00, 0x0b}
	for index := range params {
		body = append(body, 0x20)
		body = append(body, wasmtest.ULEB(uint32(index))...)
	}
	body = append(body, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, params))),
		wasmtest.Section(2, wasmtest.Vec(imports...)),
		wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("identity", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

// Imports interleave resources with actual function ordinals. Ordinary host0
// is optional; the final native function is called and then its results remain
// live across a separate Go host/GC/reentry boundary.
func rv64NativeVectorConsumer(params, results []wasm.ValType, nativeCount int, host bool) []byte {
	imports := [][]byte{wasmtest.GlobalImportEntry("env", "flag", wasm.I32, true)}
	functionCount := nativeCount
	if host {
		imports = append(imports, rv64VectorFunctionImport("observe", 1))
		functionCount++
	}
	imports = append(imports, rv64VectorMemoryImport())
	for index := 0; index < nativeCount; index++ {
		imports = append(imports, rv64VectorFunctionImport(fmt.Sprintf("f%d", index), 0))
	}
	// The marker deliberately selects the SIMD profile even for a scalar65
	// signature. Ordinary scalar-only source modules retain their64-import cap.
	body := append([]byte{0xfd, 12}, make([]byte, 16)...)
	body = append(body, 0x1a)
	for index := range params {
		body = append(body, 0x20)
		body = append(body, wasmtest.ULEB(uint32(index))...)
	}
	body = append(body, 0x10)
	body = append(body, wasmtest.ULEB(uint32(functionCount-1))...)
	if host {
		body = append(body, 0x10, 0)
	}
	body = append(body, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, results), wasmtest.FuncType(nil, nil), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(imports...)),
		wasmtest.Section(3, []byte{2, 0, 2}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, uint32(functionCount)), wasmtest.ExportEntry("safe", 0, uint32(functionCount+1)))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body), wasmtest.Code([]byte{0x41, 23, 0x0b}))),
	)
}

func rv64NativeVectorInputs(types []wasm.ValType) (input, want []uint64) {
	for index, typ := range types {
		switch typ {
		case wasm.I32:
			input = append(input, 0xfeedface80000001+uint64(index))
			want = append(want, uint64(uint32(input[len(input)-1])))
		case wasm.F32:
			input = append(input, 0xdeadbeef7f800123+uint64(index))
			want = append(want, uint64(uint32(input[len(input)-1])))
		case wasm.I64:
			input = append(input, 0xffedcba987654321+uint64(index))
			want = append(want, input[len(input)-1])
		case wasm.F64:
			input = append(input, 0x7ff0000000000001+uint64(index))
			want = append(want, input[len(input)-1])
		case wasm.V128:
			// Independent halves include raw signaling NaN and negative-zero bits.
			input = append(input, 0x7ff0000000000001+uint64(index), 0x80000000ff800123+uint64(index))
			want = append(want, input[len(input)-2:]...)
		}
	}
	return input, want
}

func TestRV64PublicNativeVectorImportTransportHostReentryAndColdReuse(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, shared := range []bool{false, true} {
			for _, shape := range []string{"pair2", "vector65", "vector128", "mixed128", "scalar65"} {
				t.Run(fmt.Sprintf("artifact%t-shared%t-%s", artifact, shared, shape), func(t *testing.T) {
					count := 128
					if shape == "pair2" {
						count = 2
					} else if shape == "vector65" || shape == "scalar65" {
						count = 65
					}
					types := make([]wasm.ValType, count)
					for index := range types {
						types[index] = wasm.V128
						if shape == "mixed128" {
							types[index] = []wasm.ValType{wasm.V128, wasm.I32, wasm.F32, wasm.I64, wasm.F64}[index%5]
						} else if shape == "scalar65" {
							types[index] = wasm.I32
						}
					}
					input, want := rv64NativeVectorInputs(types)
					var memory *Memory
					var err error
					if shared {
						_, memory = importedMemoryProvider(t)
					} else {
						memory, err = NewMemory(1, 1)
					}
					if err != nil {
						t.Fatal(err)
					}
					defer memory.Close()
					fail, flag := NewGlobalI32(0, true), NewGlobalI32(9, true)
					defer fail.Close()
					defer flag.Close()
					producerImports := NewImports().Global("env", "fail", fail)
					if shared {
						producerImports.Memory("env", "memory", memory)
					}
					producer, err := Instantiate(rv64PublicStartCompile(t, rv64NativeVectorProducer(types, shared), artifact), producerImports)
					if err != nil {
						t.Fatal(err)
					}
					defer producer.Close()
					export, err := producer.ExportedFunc("identity")
					if err != nil {
						t.Fatal(err)
					}
					c := rv64PublicStartCompile(t, rv64NativeVectorConsumer(types, types, 1, true), artifact)
					var consumer *Instance
					calls := 0
					observe := CallerHostCallFunc(func(caller Caller, _ HostCall) {
						calls++
						got, err := consumer.InvokeFromHost(context.Background(), caller, "safe")
						if err != nil || !slices.Equal(got, []uint64{23}) || len(caller.Memory()) != 65536 {
							panic("native vector result host reentry")
						}
						runtime.GC()
					})
					imports := NewImports().Global("env", "flag", flag).Memory("env", "memory", memory).Function("env", "f0", export).Function("env", "observe", observe)
					consumer, err = Instantiate(c, imports)
					if err != nil {
						t.Fatal(err)
					}
					defer consumer.Close()
					if c.executionView().syncHostSlots != uint16(max(64, len(input))) || !consumer.syncMode || (shared && consumer.jm != producer.jm) {
						t.Fatal("vector capacity/synchronous foreign ownership")
					}
					fn, err := consumer.WasmFunc("call")
					if err != nil {
						t.Fatal(err)
					}
					for attempt := 0; attempt < 3; attempt++ {
						got, err := consumer.Invoke("call", input...)
						if err != nil || !slices.Equal(got, want) {
							t.Fatal("public native vector cells", got, err)
						}
						got, err = fn.Invoke(input...)
						if err != nil || !slices.Equal(got, want) {
							t.Fatal("prepared native vector cells", got, err)
						}
						if err := fail.Set(1); err != nil {
							t.Fatal(err)
						}
						_, err = consumer.Invoke("call", input...)
						var trap *coreruntime.TrapError
						if !errors.As(err, &trap) || trap.Code != coreruntime.TrapUnreachable || len(trap.Frames) != 1 || trap.Frames[0].FunctionName != "identity" || trap.Frames[0].FunctionIndex != 0 || trap.Frames[0].ProgramCounter != 5 {
							t.Fatal("foreign vector producer source", err)
						}
						if err := fail.Set(0); err != nil {
							t.Fatal(err)
						}
					}
					if len(input) == 4 {
						session, err := fn.OpenSession()
						if err != nil {
							t.Fatal(err)
						}
						got, err := session.Invoke4(input[0], input[1], input[2], input[3])
						session.Close()
						if err != nil || !slices.Equal(got, want) {
							t.Fatal("vector session", got, err)
						}
					}
					wantCalls := 6
					if len(input) == 4 {
						wantCalls++
					}
					if calls != wantCalls {
						t.Fatal("host callback witness", calls)
					}
					if err := producer.Close(); err != nil {
						t.Fatal(err)
					}
					if got, err := consumer.Invoke("call", input...); err != nil || !slices.Equal(got, want) {
						t.Fatal("retired vector producer", got, err)
					}
					if err := consumer.Close(); err != nil || producer.jm != nil || len(consumer.thunkMem) != 0 {
						t.Fatal("native vector physical roots", err)
					}
				})
			}
		}
	}
}

func TestRV64PublicNativeVectorImportsRejectOtherProtocolsAndExactTypesBeforePublication(t *testing.T) {
	wide := make([]wasm.ValType, 65)
	for index := range wide {
		wide[index] = wasm.I32
	}
	for _, artifact := range []bool{false, true} {
		for _, types := range [][]wasm.ValType{{wasm.V128}, wide} {
			c := rv64PublicStartCompile(t, rv64NativeVectorConsumer(types, types, 1, false), artifact)
			rt := NewRuntime()
			defer rt.Close()
			// A scalar owner is intentionally incompatible with the declared shape;
			// the RV host protocol gate must run before its owner retention/validator.
			ref, err := rt.NewHostFuncRef(HostCallFunc(func(HostCall) {}), FuncSig{})
			if err != nil {
				t.Fatal(err)
			}
			defer ref.Close()
			memory, err := NewMemory(1, 1)
			if err != nil {
				t.Fatal(err)
			}
			defer memory.Close()
			flag := NewGlobalI32(0, true)
			defer flag.Close()
			bindings := []any{func() {}, ref, (*HostFuncRef)(nil)}
			if types[0] == wasm.V128 {
				bindings = append(bindings, HostCallFunc(func(HostCall) {}), CallerHostCallFunc(func(Caller, HostCall) {}))
			} else {
				for _, callback := range []any{
					HostCallFunc(func(call HostCall) { copy(call.ResultSlots(), call.ParamSlots()) }),
					CallerHostCallFunc(func(_ Caller, call HostCall) { copy(call.ResultSlots(), call.ParamSlots()) }),
				} {
					imports := NewImports().Global("env", "flag", flag).Memory("env", "memory", memory).Function("env", "f0", callback)
					in, err := Instantiate(c, imports)
					if err != nil {
						t.Fatal("wide scalar slot callback", err)
					}
					input, want := rv64NativeVectorInputs(types)
					got, invokeErr := in.Invoke("call", input...)
					closeErr := in.Close()
					if invokeErr != nil || closeErr != nil || !slices.Equal(got, want) {
						t.Fatal("wide scalar slot transport", got, invokeErr, closeErr)
					}
				}
			}
			beforeBase, beforeSealed, beforeRefs := c.codeCache.base, c.codeCache.sealed, c.codeCache.refs
			for _, binding := range bindings {
				imports := NewImports().Global("env", "flag", flag).Memory("env", "memory", memory).Function("env", "f0", binding)
				if in, err := Instantiate(c, imports); in != nil || err == nil || c.codeCache.base != beforeBase || c.codeCache.sealed != beforeSealed || c.codeCache.refs != beforeRefs || ref.importers != 0 {
					t.Fatal("vector/wide host protocol published", binding, err)
				}
			}
		}
		// These signatures occupy the same two physical cells and are deliberately
		// incompatible logical types. Slot equality must never authenticate them.
		producer, err := Instantiate(rv64PublicStartCompile(t, rv64PublicNativeIdentityModule([]wasm.ValType{wasm.I64, wasm.I64}), artifact))
		if err != nil {
			t.Fatal(err)
		}
		defer producer.Close()
		export, err := producer.ExportedFunc("identity")
		if err != nil {
			t.Fatal(err)
		}
		c := rv64PublicStartCompile(t, rv64PublicHostModule([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128}), artifact)
		before := producer.referenceLifetime().snapshot().ResourceRoots
		beforeBase, beforeSealed, beforeRefs := c.codeCache.base, c.codeCache.sealed, c.codeCache.refs
		if in, err := Instantiate(c, testImports("env.host", export)); in != nil || err == nil || c.codeCache.base != beforeBase || c.codeCache.sealed != beforeSealed || c.codeCache.refs != beforeRefs || producer.referenceLifetime().snapshot().ResourceRoots != before {
			t.Fatal("equal-cell incompatible native signature", err)
		}
	}
}

func TestRV64PublicNativeVectorImportLastOrdinalCapacityAndQuota(t *testing.T) {
	types := make([]wasm.ValType, 128)
	for index := range types {
		types[index] = wasm.V128
	}
	input, want := rv64NativeVectorInputs(types)
	for _, artifact := range []bool{false, true} {
		fail, flag := NewGlobalI32(0, true), NewGlobalI32(0, true)
		defer fail.Close()
		defer flag.Close()
		memory, err := NewMemory(1, 1)
		if err != nil {
			t.Fatal(err)
		}
		defer memory.Close()
		producer, err := Instantiate(rv64PublicStartCompile(t, rv64NativeVectorProducer(types, false), artifact), NewImports().Global("env", "fail", fail))
		if err != nil {
			t.Fatal(err)
		}
		defer producer.Close()
		export, err := producer.ExportedFunc("identity")
		if err != nil {
			t.Fatal(err)
		}
		const nativeCount = 255 // scalar host0; native target255, not section257.
		c := rv64PublicStartCompile(t, rv64NativeVectorConsumer(types, types, nativeCount, true), artifact)
		calls := 0
		imports := NewImports().Global("env", "flag", flag).Memory("env", "memory", memory).Function("env", "observe", func() { calls++ })
		for index := 0; index < nativeCount; index++ {
			imports.Function("env", fmt.Sprintf("f%d", index), export)
		}
		view := c.executionView()
		if err := view.validateCached(); err != nil {
			t.Fatal(err)
		}
		if view.syncHostSlots != 256 || railshotNativeImportArenaBytes(view, imports.bindings) != 0 {
			t.Fatal("full vector exchange/edge footprint")
		}
		need := view.arenaNeedForImports(imports.bindings, true)
		rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithMaxInstanceMetadataBytes(uint64(need - 1))))
		mod, err := rt.Module(c)
		if err != nil {
			t.Fatal(err)
		}
		before := producer.referenceLifetime().snapshot().ResourceRoots
		in, err := rt.Instantiate(context.Background(), mod, WithImports(imports))
		var limit *coreruntime.ResourceLimitError
		if in != nil || !errors.As(err, &limit) || producer.referenceLifetime().snapshot().ResourceRoots != before || calls != 0 {
			t.Fatal("vector edge quota published or retained", err)
		}
		if err := mod.Close(); err != nil {
			t.Fatal(err)
		}
		if err := rt.Close(); err != nil {
			t.Fatal(err)
		}
		in, err = Instantiate(c, imports)
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		for attempt := 0; attempt < 3; attempt++ {
			got, err := in.Invoke("call", input...)
			if err != nil || !slices.Equal(got, want) || calls != attempt+1 {
				t.Fatal("last function/full256 vector cells", got, err)
			}
		}
		if err := producer.Close(); err != nil {
			t.Fatal(err)
		}
		if got, err := in.Invoke("call", input...); err != nil || !slices.Equal(got, want) {
			t.Fatal("retired dense vector imports", got, err)
		}
		if err := in.Close(); err != nil || producer.jm != nil {
			t.Fatal("dense vector roots released", err)
		}
	}
}

func TestRV64PublicNativeVectorImportSourceArtifactFeatureAndArityGates(t *testing.T) {
	types := make([]wasm.ValType, 257)
	for index := range types {
		types[index] = wasm.V128
	}
	for _, pair := range [][2][]wasm.ValType{{types, nil}, {nil, types}} {
		if c, err := Compile(nil, rv64PublicHostModule(pair[0], pair[1])); c != nil || !errors.Is(err, railshot.ErrUnsupported) {
			t.Fatal("vector257 import bound", c, err)
		}
	}
	// An unused vector import still requires the feature and binding protocol.
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128}), wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(rv64VectorFunctionImport("host", 0))),
		wasmtest.Section(3, []byte{1, 1}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("noop", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}))),
	)
	if c, err := Compile(NewRuntimeConfig().WithFeature(CoreFeatureSIMD, false), source); c != nil || err == nil {
		t.Fatal("unused vector import lost source feature", c, err)
	}
	for _, artifact := range []bool{false, true} {
		c := rv64PublicStartCompile(t, source, artifact)
		beforeBase, beforeSealed, beforeRefs := c.codeCache.base, c.codeCache.sealed, c.codeCache.refs
		if in, err := Instantiate(c, testImports("env.host", HostCallFunc(func(HostCall) {}))); in != nil || err == nil || c.codeCache.base != beforeBase || c.codeCache.sealed != beforeSealed || c.codeCache.refs != beforeRefs {
			t.Fatal("unused vector host imported", err)
		}
		view := *c.executionView()
		view.requiredFeatures = view.requiredFeatures.SetEnabled(CoreFeatureSIMD, false)
		if err := railshotValidateCompiled(&view); !errors.Is(err, railshot.ErrUnsupported) {
			t.Fatal("artifact vector feature profile lost", err)
		}
		view = *c.executionView()
		view.importFuncSigs = append([]FuncSig(nil), view.importFuncSigs...)
		view.importFuncSigs[0].Params = make([]ValType, 129)
		for index := range view.importFuncSigs[0].Params {
			view.importFuncSigs[0].Params[index] = ValV128
		}
		if err := railshotValidateCompiled(&view); !errors.Is(err, railshot.ErrUnsupported) {
			t.Fatal("artifact vector129 import signature", err)
		}
	}
}

func rv64NativeVectorParkedProducer(types []wasm.ValType, shared bool) []byte {
	imports := [][]byte{wasmtest.GlobalImportEntry("env", "resumed", wasm.I32, true), rv64VectorFunctionImport("park", 1)}
	if shared {
		imports = append(imports, rv64VectorMemoryImport())
	}
	body := []byte{}
	for index := range types {
		body = append(body, 0x20)
		body = append(body, wasmtest.ULEB(uint32(index))...)
	}
	// All vector results remain live in producer homes through the scalar host,
	// importer reentry, GC, and interruption poll before this sentinel write.
	body = append(body, 0x10, 0, 0x23, 0, 0x41, 1, 0x6a, 0x24, 0, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(types, types), wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(imports...)),
		wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("identity", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func TestRV64PublicNativeVectorImportForeignParkReentryCancellationAndClose(t *testing.T) {
	types := make([]wasm.ValType, 128)
	for index := range types {
		types[index] = wasm.V128
	}
	input, want := rv64NativeVectorInputs(types)
	for _, artifact := range []bool{false, true} {
		for _, shared := range []bool{false, true} {
			for _, action := range []string{"return", "cancel", "close"} {
				t.Run(fmt.Sprintf("artifact%t-shared%t-%s", artifact, shared, action), func(t *testing.T) {
					var memory *Memory
					var err error
					if shared {
						_, memory = importedMemoryProvider(t)
					} else {
						memory, err = NewMemory(1, 1)
					}
					if err != nil {
						t.Fatal(err)
					}
					defer memory.Close()
					resumed, flag := NewGlobalI32(0, true), NewGlobalI32(0, true)
					defer resumed.Close()
					defer flag.Close()
					entered, release := make(chan struct{}), make(chan struct{})
					var enterOnce, releaseOnce sync.Once
					unblock := func() { releaseOnce.Do(func() { close(release) }) }
					var consumer *Instance
					park := CallerHostCallFunc(func(caller Caller, _ HostCall) {
						got, err := consumer.InvokeFromHost(context.Background(), caller, "safe")
						if err != nil || !slices.Equal(got, []uint64{23}) {
							panic("foreign vector importer reentry")
						}
						runtime.GC()
						enterOnce.Do(func() { close(entered) })
						<-release
					})
					producerImports := NewImports().Global("env", "resumed", resumed).Function("env", "park", park)
					if shared {
						producerImports.Memory("env", "memory", memory)
					}
					producer, err := Instantiate(rv64PublicStartCompile(t, rv64NativeVectorParkedProducer(types, shared), artifact), producerImports)
					if err != nil {
						t.Fatal(err)
					}
					defer producer.Close()
					export, err := producer.ExportedFunc("identity")
					if err != nil {
						t.Fatal(err)
					}
					consumer, err = Instantiate(rv64PublicStartCompile(t, rv64NativeVectorConsumer(types, types, 1, false), artifact), NewImports().Global("env", "flag", flag).Memory("env", "memory", memory).Function("env", "f0", export))
					if err != nil {
						t.Fatal(err)
					}
					defer consumer.Close()
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					type result struct {
						values []uint64
						err    error
					}
					done, joined := make(chan result, 1), make(chan struct{})
					go func() {
						defer close(joined)
						values, err := consumer.InvokeContext(ctx, "call", input...)
						done <- result{values, err}
					}()
					defer func() {
						cancel()
						unblock()
						<-joined
					}()
					select {
					case <-entered:
					case <-time.After(5 * time.Second):
						t.Fatal("foreign full-vector callback did not park")
					}
					if err := producer.Close(); err != nil || producer.jm == nil || len(consumer.thunkMem) == 0 {
						t.Fatal("foreign vector parked physical lease", err)
					}
					if action == "cancel" {
						cancel()
					} else if action == "close" {
						if err := consumer.Close(); err != nil {
							t.Fatal(err)
						}
					}
					if action != "return" {
						deadline := time.Now().Add(5 * time.Second)
						for atomic.LoadUint32((*uint32)(unsafe.Pointer(&consumer.trap[0]))) != uint32(coreruntime.TrapInterrupted) && time.Now().Before(deadline) {
							runtime.Gosched()
						}
						if atomic.LoadUint32((*uint32)(unsafe.Pointer(&consumer.trap[0]))) != uint32(coreruntime.TrapInterrupted) {
							t.Fatal("foreign vector watcher failed to interrupt")
						}
					}
					unblock()
					got := <-done
					<-joined
					if action == "return" {
						if got.err != nil || !slices.Equal(got.values, want) || resumed.Get() != 1 {
							t.Fatal("foreign parked vector cells", got, resumed.Get())
						}
					} else {
						var trap *coreruntime.TrapError
						if action == "cancel" && !errors.Is(got.err, context.Canceled) || action == "close" && (!errors.As(got.err, &trap) || trap.Code != coreruntime.TrapInterrupted) || resumed.Get() != 0 {
							t.Fatal("foreign vector interrupted post-host resume", got.err, resumed.Get())
						}
					}
					if err := consumer.Close(); err != nil || producer.jm != nil || len(consumer.thunkMem) != 0 {
						t.Fatal("foreign vector final physical release", err)
					}
				})
			}
		}
	}
}
