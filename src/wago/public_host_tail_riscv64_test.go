//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
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
	"github.com/wago-org/wago/src/core/runtime/abi"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64HostTailInstantiate(t *testing.T, rt *Runtime, c *Compiled, imports *Imports) *Instance {
	t.Helper()
	mod, err := rt.Module(c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mod.Close() })
	in, err := rt.Instantiate(context.Background(), mod, WithImports(imports))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = in.Close() })
	return in
}

func TestRV64PublicHostTailScalarTransport(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, kind := range []string{"go", "owned"} {
			for _, shape := range []string{"zero", "noargs", "noresults", "mixed", "wide64", "swar", "continuation", "typed"} {
				t.Run(fmt.Sprintf("artifact%t-%s-%s", artifact, kind, shape), func(t *testing.T) {
					types := []wasm.ValType{}
					var input, args, output, want []uint64
					switch shape {
					case "mixed", "wide64", "swar":
						count := 4
						if shape == "wide64" {
							count = 64
						}
						for i := 0; i < count; i++ {
							types = append(types, []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64}[i%4])
							input = append(input, []uint64{0xcafe123480000001, 0xfedcba9876543210, 0xcafe12347f800123, 0x7ff0000000000001}[i%4])
							output = append(output, []uint64{0xeeeeeeeefffffff7, 0xffedcba987654321, 0xeeeeeeee80000000, 0xfff8000000000123}[i%4])
						}
						args = slices.Clone(input)
						want = slices.Clone(output)
						for i := range input {
							if i%4 == 0 || i%4 == 2 {
								args[i] = uint64(uint32(args[i]))
								want[i] = uint64(uint32(want[i]))
							}
						}
					case "noargs":
						types = []wasm.ValType{wasm.I32}
						output = []uint64{0xeeeeeeee80000001}
						want = []uint64{0x80000001}
					case "noresults":
						types = []wasm.ValType{wasm.I32}
						input = []uint64{0xcafe123480000001}
						args = []uint64{0x80000001}
					case "continuation":
						types = []wasm.ValType{wasm.I32}
						input = []uint64{0xcafe123400000023}
						args = []uint64{35}
						output = []uint64{0xeeeeeeee00000023}
						want = []uint64{42}
					case "typed":
						types = []wasm.ValType{wasm.I32}
						input = []uint64{0xcafe123400000007}
						want = []uint64{8}
					}
					params, results := types, types
					if shape == "noargs" {
						params = nil
					}
					if shape == "noresults" {
						results = nil
					}
					rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
					defer rt.Close()
					calls := 0
					var host any = HostCallFunc(func(call HostCall) {
						calls++
						if !slices.Equal(call.ParamSlots(), args) {
							panic("host tail scalar arguments")
						}
						copy(call.ResultSlots(), output)
					})
					if shape == "typed" {
						host = func(v int32) int32 { calls++; return v + 1 }
					}
					var owner *HostFuncRef
					if kind == "owned" {
						signatureParams, signatureResults := valTypesFromWasm(params), valTypesFromWasm(results)
						var err error
						owner, err = rt.NewHostFuncRef(host, FuncSig{Params: signatureParams, Results: signatureResults})
						if err != nil {
							t.Fatal(err)
						}
						defer owner.Close()
						host = owner
					}
					source := rv64PublicTailModule(types, shape == "continuation")
					if shape == "noargs" || shape == "noresults" || shape == "swar" {
						source = rv64HostTailModule(params, results, shape == "swar", true)
					}
					c := rv64NativeTailCompile(t, "host-tail-"+shape, source, artifact)
					if shape == "swar" {
						m := rv64NativeTailModule(c)
						if m == nil || len(m.Plans) != 1 || m.Plans[0].SourceStride != 16 {
							t.Fatal("host edge did not use SWAR source homes")
						}
					}
					in := rv64HostTailInstantiate(t, rt, c, testImports("env.tail", host))
					if in.pluginState.Load().railshotInvocation.tailDispatcher == nil {
						t.Fatal("host tail factory absent")
					}
					if kind == "owned" && len(in.thunkMem) == 0 {
						t.Fatal("owned host tail thunk absent")
					}
					for i := 0; i < 3; i++ {
						got, err := in.Invoke("call", input...)
						if err != nil || !slices.Equal(got, want) {
							t.Fatal("host tail transport", got, want, err)
						}
					}
					tailAllocs := testing.AllocsPerRun(10, func() {
						got, err := in.Invoke("call", input...)
						if err != nil || !slices.Equal(got, want) {
							panic("warm host tail")
						}
					})
					ordinary := rv64HostTailInstantiate(t, rt, rv64NativeTailCompile(t, "host-tail-ordinary-"+shape, rv64HostTailModule(params, results, shape == "swar", false), artifact), testImports("env.tail", host))
					ordinaryAllocs := testing.AllocsPerRun(10, func() {
						if _, err := ordinary.Invoke("call", input...); err != nil {
							panic(err)
						}
					})
					extraScope := 0.0
					if kind == "owned" {
						extraScope = 1
					} // the owned adapter requires an isolated Caller scope
					if tailAllocs > ordinaryAllocs+extraScope || tailAllocs > 3 {
						t.Fatalf("tail allocations %g, ordinary host allocations %g", tailAllocs, ordinaryAllocs)
					}
					t.Logf("tail/ordinary warm allocations: %g/%g", tailAllocs, ordinaryAllocs)
					if err := ordinary.Close(); err != nil {
						t.Fatal(err)
					}
					if calls != 25 {
						t.Fatal("host call count", calls)
					}
					if err := c.Close(); err != nil {
						t.Fatal(err)
					}
					if got, err := in.Invoke("call", input...); err != nil || !slices.Equal(got, want) {
						t.Fatal("retained compiled host thunk", got, err)
					}
					if owner != nil {
						if err := owner.Close(); err == nil {
							t.Fatal("owned tail importer not retained")
						}
					}
					if err := in.Close(); err != nil {
						t.Fatal(err)
					}
					if in.pluginState.Load().railshotInvocation.tailDispatcher != nil || len(in.thunkMem) != 0 {
						t.Fatal("host tail physical retirement")
					}
					if owner != nil {
						if err := owner.Close(); err != nil {
							t.Fatal(err)
						}
					}
				})
			}
		}
	}
}

func TestRV64PublicHostTailReentryColdReuse(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, kind := range []string{"go", "owned"} {
			for _, shared := range []bool{false, true} {
				t.Run(fmt.Sprintf("artifact%t-%s-shared%t", artifact, kind, shared), func(t *testing.T) {
					rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
					defer rt.Close()
					var memory *Memory
					if shared {
						var err error
						memory, err = NewSharedMemory(1, 4)
						if err != nil {
							t.Fatal(err)
						}
						defer memory.Close()
					}
					var leaf, root *Instance
					depth, calls := 0, 0
					fail, nested := false, false
					cause := errors.New("host proper tail stopped")
					var saved Caller
					host := CallerHostCallFunc(func(caller Caller, call HostCall) {
						calls++
						if caller.in != leaf {
							panic("host tail caller identity")
						}
						if depth == 0 {
							pointer := railshotParkedNativeInvocation(leaf)
							view := rv64InvocationViewForTest(leaf)
							if pointer < root.eng.StackLimit() || pointer+64 > root.eng.RV64InvocationViewPointer() || binary.LittleEndian.Uint64(view) != abi.RV64InvocationMagic || binary.LittleEndian.Uint64(view[24:]) != uint64(pointer-4096) || binary.LittleEndian.Uint64(view[40:]) != uint64(pointer-2048) || binary.LittleEndian.Uint64(view[56:]) == 0 {
								panic("host tail record view")
							}
							before := bytes.Clone(view)
							saved = caller
							if !nested {
								nested = true
								depth++
								got, err := leaf.InvokeFromHost(context.Background(), caller, "call", 8)
								depth--
								if err != nil || !slices.Equal(got, []uint64{9}) || !bytes.Equal(view, before) {
									panic(fmt.Sprintf("host tail own engine reentry %v %v", got, err))
								}
								if got, err := root.InvokeFromHost(context.Background(), caller, "safe", 51); err != nil || !slices.Equal(got, []uint64{51}) || !bytes.Equal(view, before) {
									panic("host tail root scratch reentry")
								}
								runtime.GC()
							}
							if fail {
								panic(HostTrap{Err: cause})
							}
						}
						call.SetI32(0, call.I32(0)+1)
					})
					var binding any = host
					if kind == "owned" {
						owner, err := rt.NewHostFuncRef(host, FuncSig{Params: []ValType{ValI32}, Results: []ValType{ValI32}})
						if err != nil {
							t.Fatal(err)
						}
						defer owner.Close()
						binding = owner
					}
					imports := testImports("env.tail", binding)
					if shared {
						imports.Memory("env", "memory", memory)
					}
					key := fmt.Sprintf("host-tail-reentry-shared%t", shared)
					leaf = rv64HostTailInstantiate(t, rt, rv64NativeTailCompile(t, key+"-leaf", rv64TailHostConsumer(shared), artifact), imports)
					ex, err := leaf.ExportedFunc("call")
					if err != nil {
						t.Fatal(err)
					}
					imports = testImports("env.tail", ex)
					if shared {
						imports.Memory("env", "memory", memory)
					}
					root = rv64HostTailInstantiate(t, rt, rv64NativeTailCompile(t, key+"-root", rv64TailHostConsumer(shared), artifact), imports)
					for i := 0; i < 3; i++ {
						nested = false
						fail = false
						got, err := root.Invoke("call", 0xcafe123400000007)
						if err != nil || !slices.Equal(got, []uint64{8}) {
							t.Fatal("native to host proper tail", got, err)
						}
						if saved.valid() {
							t.Fatal("tail caller scope escaped")
						}
						nested = false
						fail = true
						if _, err := root.Invoke("call", 7); !errors.Is(err, cause) {
							t.Fatal("host tail cold trap", err)
						}
						if got, err := root.Invoke("safe", 52); err != nil || !slices.Equal(got, []uint64{52}) {
							t.Fatal("cold root entry", got, err)
						}
						if got, err := leaf.Invoke("safe", 53); err != nil || !slices.Equal(got, []uint64{53}) {
							t.Fatal("cold leaf entry", got, err)
						}
					}
					if calls != 12 {
						t.Fatal("nested proper host call count", calls)
					}
				})
			}
		}
	}
}

func rv64HostTailCloseModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "flag", wasm.I32, true), append(append(wasmtest.Name("env"), wasmtest.Name("tail")...), 0, 0))),
		wasmtest.Section(3, []byte{2, 0, 0}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 2))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x12, 0, 0x0b}), wasmtest.Code([]byte{0x20, 0, 0x10, 1, 0x24, 0, 0x23, 0, 0x0b}))),
	)
}

func TestRV64PublicHostTailCancellationAndClose(t *testing.T) {
	for _, kind := range []string{"go", "owned"} {
		for _, closeRoot := range []bool{false, true} {
			t.Run(fmt.Sprintf("artifacttrue-%s-close%t", kind, closeRoot), func(t *testing.T) {
				rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
				defer rt.Close()
				flag := NewGlobalI32(0, true)
				defer flag.Close()
				entered, release := make(chan struct{}), make(chan struct{})
				var once sync.Once
				unblock := func() { once.Do(func() { close(release) }) }
				defer unblock()
				var host any = func(v int32) int32 { close(entered); <-release; return v + 1 }
				var owner *HostFuncRef
				if kind == "owned" {
					var err error
					owner, err = rt.NewHostFuncRef(host, FuncSig{Params: []ValType{ValI32}, Results: []ValType{ValI32}})
					if err != nil {
						t.Fatal(err)
					}
					defer owner.Close()
					host = owner
				}
				c := rv64NativeTailCompile(t, "host-tail-close", rv64HostTailCloseModule(), true)
				in := rv64HostTailInstantiate(t, rt, c, testImports("env.tail", host).Global("env", "flag", flag))
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				done := make(chan error, 1)
				finished := make(chan struct{})
				go func() { defer close(finished); _, err := in.InvokeContext(ctx, "call", 7); done <- err }()
				defer func() {
					cancel()
					unblock()
					select {
					case <-finished:
					case <-time.After(5 * time.Second):
						t.Error("host tail cleanup delayed")
						<-finished
					}
				}()
				select {
				case <-entered:
				case <-time.After(5 * time.Second):
					t.Fatal("host tail did not park")
				}
				if err := c.Close(); err != nil {
					t.Fatal(err)
				}
				if closeRoot {
					if err := in.Close(); err != nil {
						t.Fatal(err)
					}
				} else {
					cancel()
				}
				if in.jm == nil || in.pluginState.Load().railshotInvocation.tailDispatcher == nil {
					t.Fatal("parked host factory retired")
				}
				if owner != nil {
					owner.mu.Lock()
					count := owner.importers
					owner.mu.Unlock()
					if count != 1 || len(in.thunkMem) == 0 {
						t.Fatal("parked owned tail thunk/owner retired")
					}
					if err := owner.Close(); err == nil {
						t.Fatal("parked tail owner closed")
					}
				}
				deadline := time.Now().Add(5 * time.Second)
				for atomic.LoadUint32((*uint32)(unsafe.Pointer(&in.trap[0]))) != uint32(coreruntime.TrapInterrupted) && time.Now().Before(deadline) {
					runtime.Gosched()
				}
				if atomic.LoadUint32((*uint32)(unsafe.Pointer(&in.trap[0]))) != uint32(coreruntime.TrapInterrupted) {
					t.Fatal("host tail watcher did not interrupt")
				}
				unblock()
				select {
				case err := <-done:
					if closeRoot {
						var trap *coreruntime.TrapError
						if !errors.As(err, &trap) || trap.Code != coreruntime.TrapInterrupted {
							t.Fatal("host tail Close unwind", err)
						}
					} else if !errors.Is(err, context.Canceled) {
						t.Fatal("host tail cancel unwind", err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("host tail did not unwind")
				}
				if flag.Get() != 0 {
					t.Fatal("interrupted host tail resumed continuation store")
				}
				if err := in.Close(); err != nil {
					t.Fatal(err)
				}
				if in.pluginState.Load().railshotInvocation.tailDispatcher != nil || len(in.thunkMem) != 0 {
					t.Fatal("host tail cleanup leaked")
				}
				if owner != nil {
					if err := owner.Close(); err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	}
}

func TestRV64PublicHostTailUnsupportedBeforePublication(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t-vector", artifact), func(t *testing.T) {
			c := rv64NativeTailCompile(t, "host-tail-reject-vector", rv64PublicTailModule([]wasm.ValType{wasm.V128}, false), artifact)
			cc := c.loadCodeCache()
			if in, err := Instantiate(c, testImports("env.tail", HostCallFunc(func(HostCall) {}))); in != nil || !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("unqualified host tail admitted", err)
			}
			if cc.sealed || cc.refs != 0 {
				t.Fatal("unqualified host tail published")
			}
		})
	}
	types := make([]wasm.ValType, 65)
	for i := range types {
		types[i] = wasm.I32
	}
	if c, err := rv64NativeTailConfig().Compile(rv64PublicTailModule(types, false)); c != nil || !errors.Is(err, railshot.ErrUnsupported) {
		t.Fatal("unqualified wide65 source", err)
	}
}

func TestRV64PublicHostTailIndependentCallbackScopes(t *testing.T) {
	for _, kind := range []string{"go", "owned"} {
		t.Run("artifacttrue-"+kind, func(t *testing.T) {
			rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
			defer rt.Close()
			entered := make(chan Caller, 3)
			release := []chan struct{}{make(chan struct{}), make(chan struct{}), make(chan struct{})}
			var once [3]sync.Once
			unblock := func(i int) { once[i].Do(func() { close(release[i]) }) }
			var binding any = CallerHostCallFunc(func(caller Caller, call HostCall) {
				index := int(call.I32(0)) - 1
				if index < 0 || index >= 3 {
					panic("callback scope index")
				}
				entered <- caller
				<-release[index]
				if !caller.valid() {
					panic("another root expired this host tail scope")
				}
				call.SetI32(0, call.I32(0)+1)
			})
			if kind == "owned" {
				owner, err := rt.NewHostFuncRef(binding, FuncSig{Params: []ValType{ValI32}, Results: []ValType{ValI32}})
				if err != nil {
					t.Fatal(err)
				}
				defer owner.Close()
				binding = owner
			}
			c := rv64NativeTailCompile(t, "host-tail-concurrent", rv64TailHostConsumer(false), true)
			leaf := rv64HostTailInstantiate(t, rt, c, testImports("env.tail", binding))
			ex, err := leaf.ExportedFunc("call")
			if err != nil {
				t.Fatal(err)
			}
			a := rv64HostTailInstantiate(t, rt, c, testImports("env.tail", ex))
			b := rv64HostTailInstantiate(t, rt, c, testImports("env.tail", ex))
			instances := []*Instance{leaf, a, b}
			type result struct {
				index int
				got   []uint64
				err   error
			}
			done := make(chan result, 3)
			var wg sync.WaitGroup
			finished := make(chan struct{})
			defer func() {
				for i := range release {
					unblock(i)
				}
				wg.Wait()
			}()
			callers := make([]Caller, 3)
			// Park the producer's own gate first, then two independent foreign roots.
			for i, in := range instances {
				wg.Add(1)
				go func(i int, in *Instance) {
					defer wg.Done()
					got, err := in.Invoke("call", uint64(i+1))
					done <- result{i, slices.Clone(got), err}
				}(i, in)
				select {
				case callers[i] = <-entered:
				case <-time.After(5 * time.Second):
					t.Fatal("independent host tail did not park", i)
				}
			}
			go func() { wg.Wait(); close(finished) }()
			retired := [3]bool{}
			for _, index := range []int{1, 0, 2} {
				unblock(index)
				select {
				case r := <-done:
					if r.index != index || r.err != nil || !slices.Equal(r.got, []uint64{uint64(index + 2)}) {
						t.Fatal("independent tail result", r)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("independent host tail did not resume", index)
				}
				retired[index] = true
				for i, caller := range callers {
					if caller.valid() == retired[i] {
						t.Fatal("independent callback generation", index, i, retired[i])
					}
				}
			}
			<-finished
		})
	}
}

// An unused pair local forces genuine SWAR lowering while its host edge remains
// scalar. Direction-specific signatures preserve exact nil root transports.
func rv64HostTailModule(params, results []wasm.ValType, vectorFrame, tail bool) []byte {
	im := append(append(wasmtest.Name("env"), wasmtest.Name("tail")...), 0, 0)
	body := []byte{}
	for i := range params {
		body = append(body, 0x20, byte(i))
	}
	op := byte(0x10)
	if tail {
		op = 0x12
	}
	body = append(body, op, 0, 0x0b)
	code := wasmtest.Code(body)
	if vectorFrame {
		fn := append([]byte{1, 1, 0x7b}, body...)
		code = append(wasmtest.ULEB(uint32(len(fn))), fn...)
	}
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, results))), wasmtest.Section(2, wasmtest.Vec(im)), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 1))), wasmtest.Section(10, wasmtest.Vec(code)))
}

func TestRV64PublicHostTailDeepNativeAndLocalChain(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig().WithNativeStackBytes(MinNativeStackBytes)))
			defer rt.Close()
			c := rv64NativeTailCompile(t, "host-tail-deep-chain", rv64PublicTailModule([]wasm.ValType{wasm.I32}, false), artifact)
			c.codeCache.setNativeStackBytes(MinNativeStackBytes)
			calls := 0
			leaf := rv64HostTailInstantiate(t, rt, c, testImports("env.tail", func(v int32) int32 { calls++; return v + 1 }))
			current := leaf
			for i := 0; i < 100; i++ {
				ex, err := current.ExportedFunc("call")
				if err != nil {
					t.Fatal(err)
				}
				current = rv64HostTailInstantiate(t, rt, c, testImports("env.tail", ex))
			}
			if current.eng.RV64InvocationViewPointer()-current.eng.StackLimit() > uintptr(MinNativeStackBytes) {
				t.Fatal("bounded host chain stack")
			}
			if got, err := current.Invoke("call", 0xcafe12340000002e); err != nil || !slices.Equal(got, []uint64{47}) {
				t.Fatal("100 native factories to host tail", got, err)
			}
			im := append(append(wasmtest.Name("env"), wasmtest.Name("tail")...), 0, 0)
			body := []byte{0x20, 0, 0x45, 0x04, 0x7f, 0x41, 47, 0x12, 0, 0x05, 0x20, 0, 0x41, 1, 0x6b, 0x12, 1, 0x0b, 0x0b}
			source := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))), wasmtest.Section(2, wasmtest.Vec(im)), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 1))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
			local := rv64NativeTailCompile(t, "host-tail-deep-local", source, artifact)
			local.codeCache.setNativeStackBytes(MinNativeStackBytes)
			in := rv64HostTailInstantiate(t, rt, local, testImports("env.tail", func(v int32) int32 { calls++; return v + 1 }))
			if got, err := in.Invoke("call", 20000); err != nil || !slices.Equal(got, []uint64{48}) {
				t.Fatal("20000 local tails to host", got, err)
			}
			if calls != 2 {
				t.Fatal("deep host callback count", calls)
			}
		})
	}
}
