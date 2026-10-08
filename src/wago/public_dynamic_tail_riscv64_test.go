//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
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

	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/riscv64"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Type 2 deliberately duplicates type 0: declared call type and function index
// belong to different namespaces. The caller's last argument selects the row.
func rv64PublicDynamicTailModule(types []wasm.ValType, imported bool) []byte {
	callerTypes := append(slices.Clone(types), wasm.I32)
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(types, types), wasmtest.FuncType(callerTypes, types), wasmtest.FuncType(types, types)))}
	if imported {
		im := append(append(wasmtest.Name("env"), wasmtest.Name("table")...), 1, 0x70, 1, 3, 3)
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(im)))
	}
	sections = append(sections, wasmtest.Section(3, []byte{2, 0, 1}))
	if !imported {
		sections = append(sections, wasmtest.Section(4, []byte{1, 0x70, 1, 3, 3}))
	}
	sections = append(sections, wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("identity", 0, 0), wasmtest.ExportEntry("call", 0, 1), wasmtest.ExportEntry("table", 1, 0))))
	if !imported {
		sections = append(sections, wasmtest.Section(9, []byte{1, 0, 0x41, 0, 0x0b, 2, 0, 1}))
	}
	var identity, call []byte
	for i := range types {
		identity = append(identity, 0x20, byte(i))
		call = append(call, 0x20, byte(i))
	}
	identity = append(identity, 0x0b)
	call = append(call, 0x20, byte(len(types)), 0x13, 2, 0, 0x0b)
	sections = append(sections, wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(identity), wasmtest.Code(call))))
	return wasmtest.Module(sections...)
}

func TestRV64PublicDynamicTailSourceArtifact(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, shape := range []string{"zero", "mixed", "pair"} {
			for _, foreign := range []bool{false, true} {
				t.Run(fmt.Sprintf("artifact%t-%s-foreign%t", artifact, shape, foreign), func(t *testing.T) {
					var types []wasm.ValType
					var input, want []uint64
					switch shape {
					case "mixed":
						types = []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64}
						input = []uint64{0xcafe123480000001, 0xfedcba9876543210, 0xcafe12347f800123, 0x7ff0000000000001}
						want = []uint64{0x80000001, input[1], 0x7f800123, input[3]}
					case "pair":
						types = []wasm.ValType{wasm.I32, wasm.V128, wasm.F32}
						input = []uint64{0xcafe123480000001, 0xfedcba9876543210, 0xff00aabbccdd1234, 0xcafe12347f800123}
						want = []uint64{0x80000001, input[1], input[2], 0x7f800123}
					}
					key := "dynamic-tail-" + shape
					var producer *Instance
					var imports *Imports
					if foreign {
						pc := rv64NativeTailCompile(t, key+"-producer", rv64PublicDynamicTailModule(types, false), artifact)
						var err error
						producer, err = Instantiate(pc)
						if err != nil {
							t.Fatal(err)
						}
						defer producer.Close()
						table, err := producer.ExportedTable("table")
						if err != nil {
							t.Fatal(err)
						}
						imports = NewImports().Table("env", "table", table)
					}
					c := rv64NativeTailCompile(t, fmt.Sprintf("%s-caller-%t", key, foreign), rv64PublicDynamicTailModule(types, foreign), artifact)
					m := rv64NativeTailModule(c)
					if m == nil || len(m.Plans) != 1 || m.Plans[0].Kind != encoder.NativeTailIndirect || m.Plans[0].TypeIndex != 2 || m.Plans[0].ReferenceBase == 0 {
						t.Fatal("lost dynamic source plan", m)
					}
					w := compiledWriter{}
					if err := railshotWriteNativeTail(&w, c); err != nil || binary.LittleEndian.Uint32(w.buf) != 2 {
						t.Fatal("symbolic version", err)
					}
					if railshotNativeTailArenaBytes(c) != rv64TailTargetsArenaBytes(c) || railshotNativeCodeBytes(c) != uint64(len(c.code)) {
						t.Fatal("unused static factory charged")
					}
					in, err := Instantiate(c, imports)
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					if producer != nil {
						if err := producer.Close(); err != nil {
							t.Fatal(err)
						}
					}
					input = append(input, 0xcafe123400000000)
					for i := 0; i < 3; i++ {
						got, err := in.Invoke("call", input...)
						if err != nil || !slices.Equal(got, want) {
							t.Fatal("dynamic transport", got, want, err)
						}
						state := &in.pluginState.Load().railshotInvocation
						if state.tailGenerationBudget.count != 0 || state.definedTailOwner != nil || rv64EntryCapability(in) != 0 {
							t.Fatal("cold exit retained charged generation")
						}
					}
					for _, index := range []uint64{1, 2, 3, 0xffffffff} {
						input[len(input)-1] = 0xcafe123400000000 | index
						if _, err := in.Invoke("call", input...); err == nil {
							t.Fatal("invalid indirect target accepted", index)
						}
					}
					input[len(input)-1] = 0
					if got, err := in.Invoke("call", input...); err != nil || !slices.Equal(got, want) {
						t.Fatal("reuse after trap", got, err)
					}
				})
			}
		}
	}
}

func TestRV64PublicDynamicTailDefaultGate(t *testing.T) {
	source := rv64PublicDynamicTailModule([]wasm.ValType{wasm.I32}, false)
	if c, err := compatibilityDefaultConfig().Compile(source); c != nil || err == nil {
		t.Fatal("public feature default opened", err)
	}
	// An ordinary native import now retains the dynamic producer graph and its
	// entry generation, even when this caller uses only a direct tail site.
	c := rv64NativeTailCompile(t, "dynamic-tail-export", source, false)
	in, err := Instantiate(c)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	export, err := in.ExportedFunc("identity")
	if err != nil {
		t.Fatal(err)
	}
	caller := rv64NativeTailCompile(t, "dynamic-tail-export-caller", rv64PublicTailModule([]wasm.ValType{wasm.I32}, false), false)
	got, err := Instantiate(caller, testImports("env.tail", export))
	if err != nil {
		t.Fatal("dynamic import attachment", err)
	}
	defer got.Close()
	if values, err := got.Invoke("call", 0xcafe123480000001); err != nil || len(values) != 1 || values[0] != 0x80000001 {
		t.Fatal("dynamic producer through static native tail", values, err)
	}
}

func rv64PublicDynamicTailCountdown(vector bool) []byte {
	return rv64PublicTailCountdown(vector, false)
}
func rv64PublicTailCountdown(vector, reference bool) []byte {
	payload := []wasm.ValType{wasm.I32, wasm.F32, wasm.I64, wasm.F64}
	if vector {
		payload = append(payload, wasm.V128)
	}
	params := append([]wasm.ValType{wasm.I64}, payload...)
	body := []byte{0x20, 0, 0x50, 0x04, 1}
	for i := range payload {
		body = append(body, 0x20, byte(i+1))
	}
	body = append(body, 0x05, 0x20, 0, 0x42, 1, 0x7d)
	for i := range payload {
		body = append(body, 0x20, byte(i+1))
	}
	if reference {
		body = append(body, 0xd2, 0, 0x15, 2, 0x0b, 0x0b)
	} else {
		body = append(body, 0x41, 0, 0x13, 2, 0, 0x0b, 0x0b)
	}
	var continuation []byte
	for i := range params {
		continuation = append(continuation, 0x20, byte(i))
	}
	continuation = append(continuation, 0x10, 0, 0x0b)
	if reference {
		return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, payload), wasmtest.FuncType(nil, payload), wasmtest.FuncType(params, payload))), wasmtest.Section(3, []byte{2, 0, 0}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0), wasmtest.ExportEntry("ordinary", 0, 1))), wasmtest.Section(9, []byte{1, 3, 0, 1, 0}), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body), wasmtest.Code(continuation))))
	}
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, payload), wasmtest.FuncType(nil, payload), wasmtest.FuncType(params, payload))), wasmtest.Section(3, []byte{2, 0, 0}), wasmtest.Section(4, []byte{1, 0x70, 1, 1, 1}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0), wasmtest.ExportEntry("ordinary", 0, 1), wasmtest.ExportEntry("table", 1, 0))), wasmtest.Section(9, []byte{1, 0, 0x41, 0, 0x0b, 1, 0}), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body), wasmtest.Code(continuation))))
}

func TestRV64PublicDynamicTailDeepPrepared(t *testing.T)   { rv64PublicTailDeepPrepared(t, false) }
func TestRV64PublicReferenceTailDeepPrepared(t *testing.T) { rv64PublicTailDeepPrepared(t, true) }
func rv64PublicTailDeepPrepared(t *testing.T, reference bool) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			t.Run(fmt.Sprintf("artifact%t-vector%t", artifact, vector), func(t *testing.T) {
				c := rv64NativeTailCompile(t, fmt.Sprintf("tail-countdown-%t-reference%t", vector, reference), rv64PublicTailCountdown(vector, reference), artifact)
				rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig().WithNativeStackBytes(MinNativeStackBytes)))
				defer rt.Close()
				in := rv64HostTailInstantiate(t, rt, c, nil)
				input := []uint64{2049, 0xcafe123480000001, 0xcafe12347f800123, 0xfedcba9876543210, 0x7ff0000000000001}
				want := []uint64{0x80000001, 0x7f800123, input[3], input[4]}
				if vector {
					input = append(input, 0xff00aabbccdd1234, 0x8765432101234567)
					want = append(want, input[5:]...)
				}
				for _, name := range []string{"run", "ordinary"} {
					fn, err := in.WasmFunc(name)
					if err != nil {
						t.Fatal(err)
					}
					session, err := fn.OpenSession()
					if err != nil {
						t.Fatal(err)
					}
					for i := 0; i < 3; i++ {
						got, err := session.Invoke(input...)
						if err != nil || !slices.Equal(got, want) {
							session.Close()
							t.Fatal(name, got, want, err)
						}
					}
					session.Close()
				}
				if in.pluginState.Load().railshotInvocation.tailGenerationBudget.count != 0 || rv64EntryCapability(in) != 0 {
					t.Fatal("prepared exit retained tail capability")
				}
			})
		}
	}
}

func rv64InfiniteDynamicTailModule() []byte { return rv64InfiniteTailModule(false) }
func rv64InfiniteTailModule(reference bool) []byte {
	if reference {
		return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))), wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "flag", wasm.I32, true))), wasmtest.Section(3, []byte{2, 0, 0}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0), wasmtest.ExportEntry("ok", 0, 1))), wasmtest.Section(9, []byte{1, 3, 0, 1, 0}), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 0, 0x41, 1, 0x6a, 0x24, 0, 0xd2, 0, 0x15, 0, 0x0b}), wasmtest.Code([]byte{0x41, 42, 0x0b}))))
	}

	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))), wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "flag", wasm.I32, true))), wasmtest.Section(3, []byte{2, 0, 0}), wasmtest.Section(4, []byte{1, 0x70, 1, 1, 1}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0), wasmtest.ExportEntry("ok", 0, 1))), wasmtest.Section(9, []byte{1, 0, 0x41, 0, 0x0b, 1, 0}), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 0, 0x41, 1, 0x6a, 0x24, 0, 0x41, 0, 0x13, 0, 0, 0x0b}), wasmtest.Code([]byte{0x41, 42, 0x0b}))))
}

func TestRV64PublicDynamicTailCancellationAndClose(t *testing.T) {
	rv64PublicTailCancellationAndClose(t, false)
}
func TestRV64PublicReferenceTailCancellationAndClose(t *testing.T) {
	rv64PublicTailCancellationAndClose(t, true)
}
func rv64PublicTailCancellationAndClose(t *testing.T, reference bool) {
	source := rv64InfiniteTailModule(reference)
	for _, artifact := range []bool{false, true} {
		for _, closeRoot := range []bool{false, true} {
			t.Run(fmt.Sprintf("artifact%t-close%t", artifact, closeRoot), func(t *testing.T) {
				c := rv64NativeTailCompile(t, fmt.Sprintf("dynamic-tail-cancel-reference%t", reference), source, artifact)
				flag := NewGlobalI32(0, true)
				defer flag.Close()
				in, err := Instantiate(c, NewImports().Global("env", "flag", flag))
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				done := make(chan error, 1)
				go func() { _, err := in.InvokeContext(ctx, "run"); done <- err }()
				joined := false
				defer func() {
					if !joined {
						cancel()
						select {
						case <-done:
						case <-time.After(5 * time.Second):
							t.Error("tail cancellation cleanup timed out")
						}
					}
				}()
				deadline := time.Now().Add(5 * time.Second)
				witness := (*uint64)(unsafe.Pointer(&flag.cell[0]))
				for atomic.LoadUint64(witness) < 2 && time.Now().Before(deadline) {
					runtime.Gosched()
				}
				if atomic.LoadUint64(witness) < 2 {
					t.Fatal("native indirect tail did not complete an exchange")
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
							t.Fatal("tail close unwind", err)
						}
					} else if !errors.Is(err, context.Canceled) {
						t.Fatal("tail cancel unwind", err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("tail interrupt timed out")
				}
				if !closeRoot {
					if got, err := in.Invoke("ok"); err != nil || len(got) != 1 || got[0] != 42 {
						t.Fatal("cancel reuse", got, err)
					}
				} else if in.jm != nil {
					t.Fatal("closed tail root retained physical memory")
				}
			})
		}
	}
}
