//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"runtime"
	"slices"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64DynamicNativeImportModule(types []wasm.ValType, mixed bool, typed ...bool) []byte {
	callerTypes := append(slices.Clone(types), wasm.I32)
	im := append(append(wasmtest.Name("env"), wasmtest.Name("target")...), 0, 0)
	var identity, args []byte
	for i := range types {
		identity = append(identity, 0x20, byte(i))
		args = append(args, 0x20, byte(i))
	}
	identity = append(identity, 0x0b)
	var call []byte
	if mixed {
		call = append(call, 0x20, byte(len(types)), 0x41, 1, 0x46, 0x04, 3)
		call = append(call, args...)
		call = append(call, 0x12, 0, 0x05)
	}

	if len(typed) != 0 && typed[0] {
		call = append(call, 0x20, byte(len(types)), 0x41, 2, 0x46, 0x04, 3)
		call = append(call, args...)
		call = append(call, 0xd2, 0, 0x15, 2, 0x05)
	}
	call = append(call, args...)
	call = append(call, 0x20, byte(len(types)), 0x13, 2, 0)
	if len(typed) != 0 && typed[0] {
		call = append(call, 0x0b)
	}
	if mixed {
		call = append(call, 0x0b)
	}
	call = append(call, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(types, types), wasmtest.FuncType(callerTypes, types), wasmtest.FuncType(types, types), wasmtest.FuncType(nil, types))),
		wasmtest.Section(2, wasmtest.Vec(im)), wasmtest.Section(3, []byte{2, 0, 1}), wasmtest.Section(4, []byte{1, 0x70, 1, 3, 3}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 2), wasmtest.ExportEntry("identity", 0, 1), wasmtest.ExportEntry("table", 1, 0))),
		wasmtest.Section(9, []byte{1, 0, 0x41, 0, 0x0b, 2, 0, 1}),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(identity), wasmtest.Code(call))),
	)
}

func TestRV64PublicDynamicNativeImportSourceArtifact(t *testing.T) {
	rv64PublicDynamicNativeImportSourceArtifact(t, false)
}
func TestRV64PublicReferenceTailMixedNativeImportSourceArtifact(t *testing.T) {
	rv64PublicDynamicNativeImportSourceArtifact(t, true)
}
func rv64PublicDynamicNativeImportSourceArtifact(t *testing.T, reference bool) {
	for _, artifact := range []bool{false, true} {
		for _, shape := range []string{"zero", "mixed", "vector"} {
			for _, mixed := range []bool{false, true} {
				t.Run(fmt.Sprintf("artifact%t-%s-mixed%t", artifact, shape, mixed), func(t *testing.T) {
					var types []wasm.ValType
					var input, want []uint64
					if shape != "zero" {
						types = []wasm.ValType{wasm.I32, wasm.F32, wasm.I64, wasm.F64}
						input = []uint64{0xcafe123480000001, 0xcafe12347f800123, 0xfedcba9876543210, 0x7ff0000000000001}
						want = []uint64{0x80000001, 0x7f800123, input[2], input[3]}
					}
					if shape == "vector" {
						types = append(types, wasm.V128)
						input = append(input, 0xaabbccddeeff0011, 0xfedcba9876543210)
						want = append(want, input[4:]...)
					}
					key := fmt.Sprintf("dynamic-native-%s-mixed%t-reference%t", shape, mixed, reference)
					pc := rv64NativeTailCompile(t, key+"-producer", rv64PublicNativeIdentityModule(types), artifact)
					producer, err := Instantiate(pc)
					if err != nil {
						t.Fatal(err)
					}
					defer producer.Close()
					export, err := producer.ExportedFunc("identity")
					if err != nil {
						t.Fatal(err)
					}
					c := rv64NativeTailCompile(t, key+"-caller", rv64DynamicNativeImportModule(types, mixed, reference), artifact)
					in, err := Instantiate(c, NewImports().Function("env", "target", export))
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					if len(producer.funcRefDescs) != 40 || binary.LittleEndian.Uint64(in.funcRefDescs[40:]) == uint64(producer.base+uintptr(producer.c.Entry[0])) {
						t.Fatal("ordinary proxy or bare producer identity changed")
					}
					if rv64StaticTailPlanCount(rv64NativeTailModule(c)) != 0 || in.pluginState.Load().railshotInvocation.tailDispatcher != nil {
						t.Fatal("mixed sites acquired competing static capability")
					}
					if err := producer.Close(); err != nil {
						t.Fatal(err)
					}
					rows := []uint64{0, 1, 0}
					if reference {
						rows = append(rows, 2)
					}
					for _, row := range rows {
						got, err := in.Invoke("call", append(slices.Clone(input), 0xcafe123400000000|row)...)
						if err != nil || !slices.Equal(got, want) {
							t.Fatal("native imported tail transport", row, got, want, err)
						}
						if state := &in.pluginState.Load().railshotInvocation; state.tailGenerationBudget.count != 0 || rv64EntryCapability(in) != 0 {
							t.Fatal("cold exit retained generation")
						}
					}
					invalid := []uint64{3, 0xffffffff}
					if !reference {
						invalid = append(invalid, 2)
					}
					for _, row := range invalid {
						if _, err := in.Invoke("call", append(slices.Clone(input), row)...); err == nil {
							t.Fatal("invalid table reference accepted", row)
						}
					}
					if got, err := in.Invoke("call", append(slices.Clone(input), 0)...); err != nil || !slices.Equal(got, want) {
						t.Fatal("reuse after imported tail trap", got, err)
					}
				})
			}
		}
	}
}

func TestRV64PublicDynamicNativeImportRejectsHost(t *testing.T) {
	c := rv64NativeTailCompile(t, "dynamic-native-host-gate", rv64DynamicNativeImportModule([]wasm.ValType{wasm.I32}, true), false)
	imports := NewImports().Function("env", "target", HostCallFunc(func(call HostCall) {}))
	if in, err := Instantiate(c, imports); in != nil || !errors.Is(err, railshot.ErrUnsupported) {
		t.Fatal("host dynamic owner published", err)
	}
}

func TestRV64PublicDynamicNativeImportResolvedAliasAndRelayGate(t *testing.T) {
	types := []wasm.ValType{wasm.I32}
	producer, err := Instantiate(rv64NativeTailCompile(t, "dynamic-native-relay-producer", rv64PublicNativeIdentityModule(types), false))
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	export, err := producer.ExportedFunc("identity")
	if err != nil {
		t.Fatal(err)
	}
	im := append(append(wasmtest.Name("env"), wasmtest.Name("target")...), 0, 0)
	relaySource := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(types, types))), wasmtest.Section(2, wasmtest.Vec(im)), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("relay", 0, 0))))
	relay, err := Instantiate(rv64NativeTailCompile(t, "dynamic-native-relay", relaySource, false), NewImports().Function("env", "target", export))
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Close()
	alias, err := relay.ExportedFunc("relay")
	if err != nil {
		t.Fatal(err)
	}
	c := rv64NativeTailCompile(t, "dynamic-native-relay-gate", rv64DynamicNativeImportModule(types, false), false)
	// ExportedFunc already resolves native reexports to their original public
	// handle. This existing API supplies a genuine definition, not a relay entry.
	if alias != export || alias.inst != producer || alias.localIdx != 0 {
		t.Fatal("public native alias changed producer identity")
	}
	before := relay.referenceLifetime().snapshot().ResourceRoots
	in, err := Instantiate(c, NewImports().Function("env", "target", alias))
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if in.pluginState.Load().railshotInvocation.nativeImports.records[0].producer != producer || relay.referenceLifetime().snapshot().ResourceRoots != before {
		t.Fatal("resolved alias retained relay instead of original producer")
	}
	// A handle actually naming an import-only relay has no genuine definition.
	forged := &InstanceExport{inst: relay, localIdx: -1, params: []ValType{ValI32}, results: []ValType{ValI32}}
	if got, err := Instantiate(c, NewImports().Function("env", "target", forged)); got != nil || err == nil {
		t.Fatal("relay entry treated as genuine framed definition", err)
	}
	if err := relay.Close(); err != nil {
		t.Fatal(err)
	}
	if got, err := in.Invoke("call", 0xcafe123480000001, 0); err != nil || len(got) != 1 || got[0] != 0x80000001 {
		t.Fatal("resolved original producer after relay close", got, err)
	}
}

func TestRV64PublicDynamicNativeImportHostParkReentry(t *testing.T) {
	rv64PublicDynamicNativeImportHostParkReentry(t, false)
}
func TestRV64PublicReferenceTailHostParkReentry(t *testing.T) {
	rv64PublicDynamicNativeImportHostParkReentry(t, true)
}
func TestRV64PublicReferenceCallHostParkReentry(t *testing.T) {
	rv64PublicDynamicNativeImportHostParkReentry(t, true, true)
}
func rv64PublicDynamicNativeImportHostParkReentry(t *testing.T, reference bool, call ...bool) {
	ordinary := len(call) != 0 && call[0]
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			var producer, in *Instance
			depth, calls := 0, 0
			fail := false
			stopped := errors.New("dynamic native tail paused host")
			host := CallerHostCallFunc(func(caller Caller, call HostCall) {
				calls++
				if depth == 0 {
					view := rv64InvocationViewForTest(producer)
					before := bytes.Clone(view)
					state := &in.pluginState.Load().railshotInvocation
					if railshotParkedNativeInvocation(producer) == 0 || state.tableDepth == 0 {
						panic("parked reference lost native owner activation")
					}
					if ordinary {
						if rv64EntryCapability(in) != 0 || state.tailGenerationBudget.count != 0 || state.tailDispatcher != nil {
							panic("ordinary reference allocated a tail factory")
						}
					} else if rv64EntryCapability(in) == 0 || state.tailGenerationBudget.count == 0 {
						panic("parked dynamic tail lost generation")
					}
					depth++
					got, err := producer.InvokeFromHost(context.Background(), caller, "producer", 8)
					depth--
					if err != nil || !slices.Equal(got, []uint64{9}) || !bytes.Equal(view, before) {
						panic(fmt.Sprintf("dynamic producer reentry %v %v", got, err))
					}
					if got, err := in.InvokeFromHost(context.Background(), caller, "identity", 51); err != nil || !slices.Equal(got, []uint64{51}) || !bytes.Equal(view, before) {
						panic("dynamic root reentry changed retained invocation view")
					}
					if fail {
						panic(HostTrap{Err: stopped})
					}
				}
				call.SetI32(0, call.I32(0)+1)
			})
			pc := rv64NativeTailCompile(t, "dynamic-native-host-producer", rv64PublicNativeHostModule(false, "producer", []byte{0x20, 0, 0x10, 0, 0x1a}), artifact)
			var err error
			producer, err = Instantiate(pc, testImports("env.host", host))
			if err != nil {
				t.Fatal(err)
			}
			defer producer.Close()
			export, err := producer.ExportedFunc("producer")
			if err != nil {
				t.Fatal(err)
			}
			source := rv64DynamicNativeImportModule([]wasm.ValType{wasm.I32}, false)
			if reference {
				source = rv64PublicReferenceTailModule([]wasm.ValType{wasm.I32}, true, ordinary)
			}
			c := rv64NativeTailCompile(t, fmt.Sprintf("dynamic-native-host-caller-reference%t-ordinary%t", reference, ordinary), source, artifact)
			in, err = Instantiate(c, NewImports().Function("env", "target", export))
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			for i := 0; i < 3; i++ {
				fail = false
				if got, err := in.Invoke("call", 0xcafe123400000007, 0); err != nil || !slices.Equal(got, []uint64{8}) {
					t.Fatal("dynamic tail host resume", got, err)
				}
				fail = true
				if _, err := in.Invoke("call", 7, 0); !errors.Is(err, stopped) {
					t.Fatal("dynamic tail host trap", err)
				}
				if got, err := in.Invoke("identity", 52); err != nil || !slices.Equal(got, []uint64{52}) {
					t.Fatal("cold reuse after nested host trap", got, err)
				}
				if in.pluginState.Load().railshotInvocation.tailGenerationBudget.count != 0 || rv64EntryCapability(in) != 0 {
					t.Fatal("parked generations escaped cold cleanup")
				}
			}
			if calls != 27 {
				t.Fatal("expected two host pauses and nested reentry", calls)
			}
		})
	}
}

func rv64OrdinaryNativeCallerModule(params, results []wasm.ValType, tail ...bool) []byte {
	im := append(append(wasmtest.Name("env"), wasmtest.Name("target")...), 0, 0)
	var body []byte
	for i := range params {
		body = append(body, 0x20, byte(i))
	}
	opcode := byte(0x10)
	if len(tail) != 0 && tail[0] {
		opcode = 0x12
	}
	body = append(body, opcode, 0, 0x0b)
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, results))), wasmtest.Section(2, wasmtest.Vec(im)), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 1))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
}

func TestRV64PublicDynamicNativeImportOrdinaryBridgeDeepPrepared(t *testing.T) {
	rv64PublicNativeImportDeepPrepared(t, false)
}
func TestRV64PublicReferenceTailOrdinaryBridgeDeepPrepared(t *testing.T) {
	rv64PublicNativeImportDeepPrepared(t, true)
}
func rv64PublicNativeImportDeepPrepared(t *testing.T, reference bool) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			t.Run(fmt.Sprintf("artifact%t-vector%t", artifact, vector), func(t *testing.T) {
				payload := []wasm.ValType{wasm.I32, wasm.F32, wasm.I64, wasm.F64}
				if vector {
					payload = append(payload, wasm.V128)
				}
				params := append([]wasm.ValType{wasm.I64}, payload...)
				key := fmt.Sprintf("dynamic-native-ordinary-%t-reference%t", vector, reference)
				pc := rv64NativeTailCompile(t, key+"-producer", rv64PublicTailCountdown(vector, reference), artifact)
				producer, err := Instantiate(pc)
				if err != nil {
					t.Fatal(err)
				}
				defer producer.Close()
				export, err := producer.ExportedFunc("run")
				if err != nil {
					t.Fatal(err)
				}
				c := rv64NativeTailCompile(t, key+"-caller", rv64OrdinaryNativeCallerModule(params, payload), artifact)
				rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig().WithNativeStackBytes(MinNativeStackBytes)))
				defer rt.Close()
				in := rv64HostTailInstantiate(t, rt, c, NewImports().Function("env", "target", export))
				state := &in.pluginState.Load().railshotInvocation
				if c.needsFuncRefDescs() || state.nativeImports == nil || !state.nativeImports.published || len(state.nativeImports.records) != 0 || !state.tailPolicy.ready {
					t.Fatal("table-free importer lost header-only publication/policy")
				}
				staticC := rv64NativeTailCompile(t, key+"-static", rv64OrdinaryNativeCallerModule(params, payload, true), artifact)
				staticIn := rv64HostTailInstantiate(t, rt, staticC, NewImports().Function("env", "target", export))
				if err := producer.Close(); err != nil {
					t.Fatal(err)
				}
				input := []uint64{2049, 0xcafe123480000001, 0xcafe12347f800123, 0xfedcba9876543210, 0x7ff0000000000001}
				want := []uint64{0x80000001, 0x7f800123, input[3], input[4]}
				if vector {
					input = append(input, 0xaabbccddeeff0011, 0xffeeccbb00112233)
					want = append(want, input[5:]...)
				}
				if got, err := staticIn.Invoke("call", input...); err != nil || !slices.Equal(got, want) {
					t.Fatal("static tail into dynamic countdown", got, want, err)
				}
				fn, err := in.WasmFunc("call")
				if err != nil {
					t.Fatal(err)
				}
				session, err := fn.OpenSession()
				if err != nil {
					t.Fatal(err)
				}
				defer session.Close()
				for i := 0; i < 3; i++ {
					if got, err := session.Invoke(input...); err != nil || !slices.Equal(got, want) {
						t.Fatal("ordinary bridge continuation after 2049 dynamic tails", got, want, err)
					}
				}
				if state.tailGenerationBudget.count != 0 || producer.pluginState.Load().railshotInvocation.tailGenerationBudget.count != 0 || rv64EntryCapability(producer) != 0 {
					t.Fatal("ordinary bridge cold exit retained generation")
				}
			})
		}
	}
}

func TestRV64PublicDynamicNativeImportCancellationAndClose(t *testing.T) {
	run := append(append(wasmtest.Name("env"), wasmtest.Name("run")...), 0, 0)
	ok := append(append(wasmtest.Name("env"), wasmtest.Name("ok")...), 0, 0)
	source := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))), wasmtest.Section(2, wasmtest.Vec(run, ok)), wasmtest.Section(3, []byte{2, 0, 0}), wasmtest.Section(4, []byte{1, 0x70, 1, 1, 1}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 2), wasmtest.ExportEntry("safe", 0, 3))), wasmtest.Section(9, []byte{1, 0, 0x41, 0, 0x0b, 1, 0}), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0, 0x13, 0, 0, 0x0b}), wasmtest.Code([]byte{0x10, 1, 0x0b}))))
	for _, artifact := range []bool{false, true} {
		for _, closeRoot := range []bool{false, true} {
			t.Run(fmt.Sprintf("artifact%t-close%t", artifact, closeRoot), func(t *testing.T) {
				pc := rv64NativeTailCompile(t, "dynamic-native-cancel-producer", rv64InfiniteDynamicTailModule(), artifact)
				flag := NewGlobalI32(0, true)
				defer flag.Close()
				producer, err := Instantiate(pc, NewImports().Global("env", "flag", flag))
				if err != nil {
					t.Fatal(err)
				}
				defer producer.Close()
				exports := NewImports()
				for _, name := range []string{"run", "ok"} {
					f, err := producer.ExportedFunc(name)
					if err != nil {
						t.Fatal(err)
					}
					exports.Function("env", name, f)
				}
				c := rv64NativeTailCompile(t, "dynamic-native-cancel-caller", source, artifact)
				in, err := Instantiate(c, exports)
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				if err := producer.Close(); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				done := make(chan error, 1)
				go func() { _, err := in.InvokeContext(ctx, "call"); done <- err }()
				joined := false
				defer func() {
					if !joined {
						cancel()
						select {
						case <-done:
						case <-time.After(5 * time.Second):
							t.Error("imported tail cleanup timed out")
						}
					}
				}()
				deadline := time.Now().Add(5 * time.Second)
				witness := (*uint64)(unsafe.Pointer(&flag.cell[0]))
				for atomic.LoadUint64(witness) < 2 && time.Now().Before(deadline) {
					runtime.Gosched()
				}
				if atomic.LoadUint64(witness) < 2 {
					t.Fatal("imported native tail did not exchange")
				}
				if closeRoot {
					if err := in.Close(); err != nil {
						t.Fatal(err)
					}
				} else {
					cancel()
				}
				select {
				case err := <-done:
					joined = true
					if closeRoot {
						var trap *coreruntime.TrapError
						if !errors.As(err, &trap) || trap.Code != coreruntime.TrapInterrupted {
							t.Fatal("imported tail close unwind", err)
						}
					} else if !errors.Is(err, context.Canceled) {
						t.Fatal("imported tail cancel unwind", err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("imported tail interrupt timed out")
				}
				if !closeRoot {
					if got, err := in.Invoke("safe"); err != nil || len(got) != 1 || got[0] != 42 {
						t.Fatal("imported tail cancel reuse", got, err)
					}
				} else if !in.resourcesClosed || !producer.resourcesClosed {
					t.Fatal("closed imported tail retained physical graph")
				}
			})
		}
	}
}

func TestRV64PublicDynamicNativeImportCancellationAndCloseWhileProducerParked(t *testing.T) {
	rv64TailCancellationAndCloseWhileProducerParked(t, true, false)
}

func TestRV64PublicReferenceTailCancellationAndCloseWhileProducerParked(t *testing.T) {
	rv64TailCancellationAndCloseWhileProducerParked(t, false, true)
}
