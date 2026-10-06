//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

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

func rv64PublicStartCompile(t *testing.T, source []byte, artifact bool) *Compiled {
	t.Helper()
	c, err := Compile(nil, source)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if !artifact {
		return c
	}
	blob, err := c.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadTrustedArtifact(blob)
	if loaded != nil {
		t.Cleanup(func() { _ = loaded.Close() })
	}
	if err != nil {
		t.Fatal(err)
	}
	return loaded
}

func TestRV64PublicScalarDefinedStartRunsOncePerInstance(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, []byte{2, 0, 1}), wasmtest.Section(5, []byte{1, 0, 1}),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 5, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("get", 0, 1))), wasmtest.Section(8, []byte{0}),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 0, 0x41, 0, 0x2d, 0, 0, 0x6a, 0x24, 0, 0x0b}), wasmtest.Code([]byte{0x23, 0, 0x0b}))),
		wasmtest.Section(11, []byte{1, 0, 0x41, 0, 0x0b, 1, 7}),
	)
	for _, artifact := range []bool{false, true} {
		for _, ownedRuntime := range []bool{false, true} {
			t.Run(fmt.Sprintf("artifact%t-runtime%t", artifact, ownedRuntime), func(t *testing.T) {
				c := rv64PublicStartCompile(t, source, artifact)
				var rt *Runtime
				var mod *Module
				if ownedRuntime {
					rt = NewRuntime()
					defer rt.Close()
					var err error
					mod, err = rt.Module(c)
					if err != nil {
						t.Fatal(err)
					}
					defer mod.Close()
				}
				for range 2 {
					var in *Instance
					var err error
					if rt != nil {
						in, err = rt.Instantiate(context.Background(), mod)
					} else {
						in, err = Instantiate(c)
					}
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					for range 3 {
						importedGlobalInvoke(t, in, "get", []uint64{12})
					}
					if err := in.Close(); err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	}
}

// Every defined function is lowered normally. The start reads initialized
// data/globals/elements, updates imported cells, then crosses the chosen tail.
func rv64PublicStateStartModule(table, host bool, tail []byte) []byte {
	imports := [][]byte{wasmtest.GlobalImportEntry("env", "v", wasm.V128, true), append(append(wasmtest.Name("env"), wasmtest.Name("memory")...), 2, 1, 1, 4)}
	functionImports := byte(0)
	if host {
		functionImports = 1
		imports = append(imports, append(append(wasmtest.Name("env"), wasmtest.Name("host")...), 0, 0))
	}
	imports = append(imports, wasmtest.GlobalImportEntry("env", "flag", wasm.I32, true))
	sections := [][]byte{
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32, wasm.V128}), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(imports...)), wasmtest.Section(3, []byte{3, 0, 1, 2}),
	}
	if table {
		sections = append(sections, wasmtest.Section(4, []byte{1, 0x70, 1, 1, 2}))
	}
	literal := append(append([]byte{0xfd, 12}, make([]byte, 16)...), 0x0b)
	sections = append(sections,
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 5, 0x0b}), wasmtest.GlobalEntry(wasm.V128, true, literal))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("get", 0, uint32(functionImports)+1))), wasmtest.Section(8, []byte{functionImports}),
	)
	if table {
		sections = append(sections, wasmtest.Section(9, wasmtest.Vec([]byte{0, 0x41, 0, 0x0b, 1, 2})))
	}
	start := []byte{0x23, 2, 0x41, 0, 0x2d, 0, 0, 0x6a, 0x23, 1, 0x6a}
	if table {
		start = append(start, 0x41, 0, 0x11, 2, 0, 0x6a)
	}
	start = append(start, 0x24, 2, 0x23, 0, 0x24, 3, 0x23, 1, 0x41, 1, 0x6a, 0x24, 1, 0x41, 1, 0x23, 2, 0x3a, 0, 0)
	if host {
		start = append(start, 0x10, 0)
	}
	start = append(start, tail...)
	start = append(start, 0x41, 2, 0x41, 0xe3, 0, 0x3a, 0, 0, 0x0b)
	sections = append(sections, wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(start), wasmtest.Code([]byte{0x23, 2, 0x23, 3, 0x0b}), wasmtest.Code([]byte{0x23, 2, 0x0b}))), wasmtest.Section(11, []byte{1, 0, 0x41, 0, 0x0b, 3, 7, 0, 0}))
	return wasmtest.Module(sections...)
}

func rv64PublicStartImports(t *testing.T) (*Instance, *Memory, *Global, *Global, *Imports) {
	t.Helper()
	producer, memory := importedMemoryProvider(t)
	vector := NewGlobalV128(rv64PublicVector(0x80000000deadbeef, 0x7ff0000000000001), true)
	flag := NewGlobalI32(9, true)
	t.Cleanup(func() { _ = vector.Close(); _ = flag.Close() })
	return producer, memory, vector, flag, NewImports().Global("env", "v", vector).Global("env", "flag", flag).Memory("env", "memory", memory)
}

func TestRV64PublicDefinedStateStartOrderingNestedHostAndArtifacts(t *testing.T) {
	for _, table := range []bool{false, true} {
		for _, artifact := range []bool{false, true} {
			for _, ownedRuntime := range []bool{false, true} {
				t.Run(fmt.Sprintf("table%t-artifact%t-runtime%t", table, artifact, ownedRuntime), func(t *testing.T) {
					producer, memory, _, flag, imports := rv64PublicStartImports(t)
					calls := 0
					imports.Function("env", "host", CallerHostCallFunc(func(caller Caller, call HostCall) {
						if call.ParamCount() != 0 || call.ResultCount() != 0 || caller.Memory()[0] != 7 || caller.Memory()[1] != byte(21+calls) || flag.Get() != uint64(10+calls) {
							panic("start did not observe initialized state before host")
						}
						if _, err := producer.Invoke("bump"); err != nil {
							panic(HostTrap{Err: err})
						}
						calls++
						runtime.GC()
					}))
					c := rv64PublicStartCompile(t, rv64PublicStateStartModule(table, !table, nil), artifact)
					if c.StartLocalFunc != 0 || c.StartIsImport || c.NumImports != map[bool]int{false: 1, true: 0}[table] {
						t.Fatal("defined start identity across interleaved imports")
					}
					var rt *Runtime
					var mod *Module
					if ownedRuntime {
						rt = NewRuntime()
						defer rt.Close()
						var err error
						mod, err = rt.Module(c)
						if err != nil {
							t.Fatal(err)
						}
						defer mod.Close()
					}
					for attempt := range 2 {
						var in *Instance
						var err error
						if rt != nil {
							in, err = rt.Instantiate(context.Background(), mod, WithImports(imports))
						} else {
							in, err = Instantiate(c, imports)
						}
						if err != nil {
							t.Fatal(err)
						}
						defer in.Close()
						counter := uint64(21 + attempt)
						if table {
							counter += 5
						}
						for range 3 {
							importedGlobalInvoke(t, in, "get", []uint64{counter, 0x80000000deadbeef, 0x7ff0000000000001})
						}
						if memory.UnsafeBytes()[1] != byte(counter) || memory.UnsafeBytes()[2] != 99 || flag.Get() != uint64(10+attempt) {
							t.Fatal("once-per-instance initialized start effects")
						}
						if err := in.Close(); err != nil {
							t.Fatal(err)
						}
					}
					if !table && calls != 2 {
						t.Fatal("start host invocation count", calls)
					}
				})
			}
		}
	}
}

func TestRV64PublicDefinedStartMetadataGatesBeforePublication(t *testing.T) {
	c := rv64PublicStartCompile(t, rv64PublicStateStartModule(false, true, nil), false)
	for _, mutate := range []func(*Compiled){
		func(v *Compiled) { v.StartIsImport = true; v.StartImportIdx = len(v.Imports) },
		func(v *Compiled) { v.StartLocalFunc = -1 },
		func(v *Compiled) { v.StartLocalFunc = len(v.Funcs) },
		func(v *Compiled) { v.StartLocalFunc = 1 },
		func(v *Compiled) { v.Entry = nil },
		func(v *Compiled) { v.Funcs = slices.Clone(v.Funcs); v.Funcs[0].Params = []ValType{ValI32} },
	} {
		view := *c.executionView()
		mutate(&view)
		if err := railshotValidateCompiled(&view); !errors.Is(err, railshot.ErrUnsupported) {
			t.Fatal("invalid defined-start artifact admitted", err)
		}
		if c.codeCache.base != 0 || c.codeCache.sealed {
			t.Fatal("invalid start published code")
		}
	}
}

func TestRV64PublicDefinedStartTrapsRetainExternalEffectsAndReleaseRoots(t *testing.T) {
	for _, test := range []struct {
		name string
		tail []byte
		trap coreruntime.TrapCode
	}{{"unreachable", []byte{0}, coreruntime.TrapUnreachable}, {"division", []byte{0x41, 1, 0x41, 0, 0x6d, 0x1a}, coreruntime.TrapDivZero}, {"memory", []byte{0x41, 0x7f, 0x2d, 0, 0, 0x1a}, coreruntime.TrapLinMemOutOfBounds}, {"recursion", []byte{0x10, 0}, coreruntime.TrapStackFenceBreached}} {
		for _, artifact := range []bool{false, true} {
			for _, ownedRuntime := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s-artifact%t-runtime%t", test.name, artifact, ownedRuntime), func(t *testing.T) {
					producer, memory, vector, flag, imports := rv64PublicStartImports(t)
					c := rv64PublicStartCompile(t, rv64PublicStateStartModule(false, false, test.tail), artifact)
					var rt *Runtime
					var mod *Module
					if ownedRuntime {
						rt = NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithNativeStackBytes(MinNativeStackBytes)))
						defer rt.Close()
						var err error
						mod, err = rt.Module(c)
						if err != nil {
							t.Fatal(err)
						}
						defer mod.Close()
					}
					if test.name != "recursion" {
						for range 2 {
							if err := flag.Set(9); err != nil {
								t.Fatal(err)
							}
							var in *Instance
							var err error
							if rt != nil {
								in, err = rt.Instantiate(context.Background(), mod, WithImports(imports))
							} else {
								in, err = Instantiate(c, InstantiateOptions{Imports: imports, nativeStackBytes: MinNativeStackBytes})
							}
							if in != nil {
								defer in.Close()
								t.Fatal("trapping start returned instance")
							}
							ownedTableTrap(t, err, test.trap)
							if memory.UnsafeBytes()[0] != 7 || memory.UnsafeBytes()[1] != 21 || memory.UnsafeBytes()[2] != 0 || flag.Get() != 10 {
								t.Fatal("failed start rolled back prior effects or resumed past trap")
							}
							if err := vector.SetV128(rv64PublicVector(0x80000000deadbeef, 0x7ff0000000000001)); err != nil {
								t.Fatal(err)
							}
						}
					} else {
						var in *Instance
						var err error
						if rt != nil {
							in, err = rt.Instantiate(context.Background(), mod, WithImports(imports))
						} else {
							in, err = Instantiate(c, InstantiateOptions{Imports: imports, nativeStackBytes: MinNativeStackBytes})
						}
						if in != nil {
							defer in.Close()
							t.Fatal("recursive start returned instance")
						}
						ownedTableTrap(t, err, test.trap)
					}
					importedGlobalInvoke(t, producer, "bump", []uint64{11})
					if err := vector.Close(); err != nil {
						t.Fatal("start failure leaked vector roots", err)
					}
					if err := flag.Close(); err != nil {
						t.Fatal("start failure leaked scalar roots", err)
					}
					if err := producer.Close(); err != nil || memory.jobMemory() != nil {
						t.Fatal("start failure leaked memory owner", err)
					}
				})
			}
		}
	}
}

func TestRV64PublicDefinedStartCancellationAndHostPanic(t *testing.T) {
	previous := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previous)
	for _, artifact := range []bool{false, true} {
		for _, behavior := range []string{"pre-cancel", "loop", "park", "panic"} {
			t.Run(fmt.Sprintf("artifact%t-%s", artifact, behavior), func(t *testing.T) {
				producer, memory, vector, flag, imports := rv64PublicStartImports(t)
				var tail []byte
				if behavior == "loop" {
					tail = []byte{3, 0x40, 0x41, 32, 0x41, 1, 0x36, 2, 0, 0x0c, 0, 0x0b}
				}
				c := rv64PublicStartCompile(t, rv64PublicStateStartModule(false, behavior == "park" || behavior == "panic", tail), artifact)
				entered, release := make(chan *Instance, 1), make(chan struct{})
				var once sync.Once
				unblock := func() { once.Do(func() { close(release) }) }
				defer unblock()
				cause := errors.New("defined start host panic")
				imports.Function("env", "host", CallerHostCallFunc(func(caller Caller, _ HostCall) {
					if _, err := producer.Invoke("bump"); err != nil {
						panic(HostTrap{Err: err})
					}
					runtime.GC()
					if behavior == "panic" {
						panic(HostTrap{Err: cause})
					}
					entered <- caller.in
					<-release
				}))
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				if behavior == "pre-cancel" {
					cancel()
				}
				type outcome struct {
					in  *Instance
					err error
				}
				done := make(chan outcome, 1)
				joined := false
				defer func() {
					cancel()
					unblock()
					if !joined {
						result := <-done
						if result.in != nil {
							_ = result.in.Close()
						}
					}
				}()
				go func() {
					in, err := Instantiate(c, InstantiateOptions{Imports: imports, startContext: ctx, nativeStackBytes: MinNativeStackBytes})
					done <- outcome{in, err}
				}()
				if behavior == "park" {
					var active *Instance
					select {
					case active = <-entered:
					case <-time.After(5 * time.Second):
						t.Fatal("defined start callback did not park")
					}
					cancel()
					deadline := time.Now().Add(5 * time.Second)
					for atomic.LoadUint32((*uint32)(unsafe.Pointer(&active.trap[0]))) != uint32(coreruntime.TrapInterrupted) && time.Now().Before(deadline) {
						runtime.Gosched()
					}
					if atomic.LoadUint32((*uint32)(unsafe.Pointer(&active.trap[0]))) != uint32(coreruntime.TrapInterrupted) {
						t.Fatal("defined start watcher did not publish interruption")
					}
					unblock()
				} else if behavior == "loop" {
					active := (*uint32)(unsafe.Pointer(&memory.UnsafeBytes()[32]))
					deadline := time.Now().Add(5 * time.Second)
					for atomic.LoadUint32(active) == 0 && time.Now().Before(deadline) {
						runtime.Gosched()
					}
					if atomic.LoadUint32(active) == 0 {
						t.Fatal("defined start loop did not execute")
					}
					cancel()
				}
				select {
				case result := <-done:
					joined = true
					if result.in != nil {
						defer result.in.Close()
						t.Fatal("failed defined start returned instance")
					}
					want := error(context.Canceled)
					if behavior == "panic" {
						want = cause
					}
					if !errors.Is(result.err, want) {
						t.Fatal("defined start cancellation/panic cause", result.err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("defined start failed to unwind")
				}
				if behavior == "pre-cancel" {
					if flag.Get() != 9 || memory.UnsafeBytes()[0] != 0 {
						t.Fatal("pre-cancelled start initialized state")
					}
				} else if flag.Get() != 10 || memory.UnsafeBytes()[1] != 21 || memory.UnsafeBytes()[2] != 0 {
					t.Fatal("failed start effects or post-host resume")
				}
				if err := vector.Close(); err != nil {
					t.Fatal("cancelled start vector roots", err)
				}
				if err := flag.Close(); err != nil {
					t.Fatal("cancelled start scalar roots", err)
				}
				if err := producer.Close(); err != nil || memory.jobMemory() != nil {
					t.Fatal("cancelled start memory roots", err)
				}
			})
		}
	}
}

func TestRV64PublicDefinedStartCloseDuringExecution(t *testing.T) {
	previous := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previous)
	for _, artifact := range []bool{false, true} {
		for _, behavior := range []string{"instance-park", "instance-self", "runtime-park", "runtime-self", "runtime-loop"} {
			t.Run(fmt.Sprintf("artifact%t-%s", artifact, behavior), func(t *testing.T) {
				producer, memory, vector, flag, imports := rv64PublicStartImports(t)
				ownedRuntime := behavior == "runtime-park" || behavior == "runtime-self" || behavior == "runtime-loop"
				loop := behavior == "runtime-loop"
				self := behavior == "instance-self" || behavior == "runtime-self"
				var tail []byte
				if loop {
					tail = []byte{3, 0x40, 0x41, 32, 0x41, 1, 0x36, 2, 0, 0x0c, 0, 0x0b}
				}
				c := rv64PublicStartCompile(t, rv64PublicStateStartModule(false, !loop, tail), artifact)
				var rt *Runtime
				var mod *Module
				if ownedRuntime {
					rt = NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithNativeStackBytes(MinNativeStackBytes)))
					defer rt.Close()
					var err error
					mod, err = rt.Module(c)
					if err != nil {
						t.Fatal(err)
					}
					defer mod.Close()
				}
				entered, release := make(chan *Instance, 1), make(chan struct{})
				var once sync.Once
				unblock := func() { once.Do(func() { close(release) }) }
				imports.Function("env", "host", CallerHostCallFunc(func(caller Caller, _ HostCall) {
					if _, err := producer.Invoke("bump"); err != nil {
						panic(HostTrap{Err: err})
					}
					if self {
						var err error
						if ownedRuntime {
							err = rt.Close()
						} else {
							err = caller.in.Close()
						}
						if err != nil {
							panic(HostTrap{Err: err})
						}
					}
					entered <- caller.in
					<-release
				}))
				type outcome struct {
					in  *Instance
					err error
				}
				done := make(chan outcome, 1)
				joined := false
				ctx, cancel := context.WithCancel(context.Background())
				defer func() {
					cancel()
					unblock()
					if !joined {
						result := <-done
						if result.in != nil {
							_ = result.in.Close()
						}
					}
				}()
				go func() {
					var in *Instance
					var err error
					if ownedRuntime {
						in, err = rt.Instantiate(ctx, mod, WithImports(imports))
					} else {
						in, err = Instantiate(c, InstantiateOptions{Imports: imports, startContext: ctx, nativeStackBytes: MinNativeStackBytes})
					}
					done <- outcome{in, err}
				}()
				if loop {
					cell := (*uint32)(unsafe.Pointer(&memory.UnsafeBytes()[32]))
					deadline := time.Now().Add(5 * time.Second)
					for atomic.LoadUint32(cell) == 0 && time.Now().Before(deadline) {
						runtime.Gosched()
					}
					if atomic.LoadUint32(cell) == 0 {
						t.Fatal("start loop did not execute")
					}
					if err := rt.Close(); err != nil {
						t.Fatal(err)
					}
				} else {
					var active *Instance
					select {
					case active = <-entered:
					case <-time.After(5 * time.Second):
						t.Fatal("start callback did not park")
					}
					if !self {
						var err error
						if ownedRuntime {
							err = rt.Close()
						} else {
							err = active.Close()
						}
						if err != nil {
							t.Fatal(err)
						}
					}
					deadline := time.Now().Add(5 * time.Second)
					for atomic.LoadUint32((*uint32)(unsafe.Pointer(&active.trap[0]))) != uint32(coreruntime.TrapInterrupted) && time.Now().Before(deadline) {
						runtime.Gosched()
					}
					if atomic.LoadUint32((*uint32)(unsafe.Pointer(&active.trap[0]))) != uint32(coreruntime.TrapInterrupted) {
						t.Fatal("Close did not interrupt active start")
					}
					active.lifeMu.Lock()
					released := active.resourcesClosed || !active.constructionActive || active.eng == nil
					active.lifeMu.Unlock()
					if released {
						t.Fatal("parked start released construction resources")
					}
					unblock()
				}
				select {
				case result := <-done:
					joined = true
					if result.in != nil {
						defer result.in.Close()
						t.Fatal("closed start returned instance")
					}
					ownedTableTrap(t, result.err, coreruntime.TrapInterrupted)
				case <-time.After(5 * time.Second):
					t.Fatal("closed start did not unwind")
				}
				if ownedRuntime {
					wait, stop := context.WithTimeout(context.Background(), 5*time.Second)
					defer stop()
					if err := rt.WaitClosed(wait); err != nil {
						t.Fatal(err)
					}
				}
				if flag.Get() != 10 || memory.UnsafeBytes()[1] != 21 || memory.UnsafeBytes()[2] != 0 {
					t.Fatal("closed start resumed after interruption")
				}
				if err := vector.Close(); err != nil {
					t.Fatal("closed start vector roots", err)
				}
				if err := flag.Close(); err != nil {
					t.Fatal("closed start scalar roots", err)
				}
				if err := producer.Close(); err != nil || memory.jobMemory() != nil {
					t.Fatal("closed start memory roots", err)
				}
				// Reuse pooled engines and arenas after the closed constructor unwinds.
				plain := rv64PublicStartCompile(t, wasmtest.Module(wasmtest.Section(1, []byte{1, 0x60, 0, 0}), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(8, []byte{0}), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b})))), artifact)
				for range 3 {
					in, err := Instantiate(plain)
					if err != nil {
						t.Fatal("reuse after closed start", err)
					}
					if err := in.Close(); err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	}
}

func TestRV64PublicDefinedStartTrapFramesIncludeImportedPrefixAndLocals(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		// One i64 local declaration consumes three bytes before the first opcode.
		body := []byte{1, 1, wasm.MustEncodeValType(wasm.I64), 0, 0x0b}
		source := wasmtest.Module(wasmtest.Section(1, []byte{1, 0x60, 0, 0}), wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("env"), wasmtest.Name("unused")...), 0, 0))), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(8, []byte{1}), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))))
		c := rv64PublicStartCompile(t, source, artifact)
		in, err := Instantiate(c, NewImports().Function("env", "unused", HostCallFunc(func(HostCall) {})))
		if in != nil {
			defer in.Close()
			t.Fatal("trapping start returned instance")
		}
		var trap *TrapError
		if !errors.As(err, &trap) || trap.Code != coreruntime.TrapUnreachable || len(trap.Frames) != 1 || trap.Frames[0].FunctionIndex != 1 || !trap.Frames[0].HasProgramCounter || trap.Frames[0].ProgramCounter != 3 {
			t.Fatal("start trap frame", err, trap)
		}
	}
}
