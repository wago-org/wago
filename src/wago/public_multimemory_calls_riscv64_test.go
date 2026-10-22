//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Both call participants own two different bounded memories. Function ordinal0
// is imported; the same scalar bodies also run through the SWAR module walker.
func rv64MMCallModule(producer, swar bool) []byte {
	types := [][]byte{wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, nil), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})}
	if swar {
		types = append(types, wasmtest.FuncType([]wasm.ValType{wasm.V128}, nil))
	}
	load := append([]byte{0x20, 0}, rv64MMArg([]byte{0x28}, 1, 0)...)
	store := append([]byte{0x20, 0, 0x20, 1}, rv64MMArg([]byte{0x36}, 1, 0)...)
	run := []byte{0x20, 0, 0x10, 0}
	name := "call"
	if producer {
		name = "produce"
		run = append(run, 0x1a)
	}
	run = append(run, load...)
	if !producer {
		run = append(run, 0x6a)
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("env"), wasmtest.Name("host")...), 0, 0))),
		wasmtest.Section(3, []byte{5, 0, 0, 0, 1, 2}),
		wasmtest.Section(5, []byte{2, 1, 1, 3, 1, 1, 3}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry(name, 0, 1), wasmtest.ExportEntry("load", 0, 2), wasmtest.ExportEntry("grow", 0, 3), wasmtest.ExportEntry("store", 0, 4), wasmtest.ExportEntry("size", 0, 5), wasmtest.ExportEntry("m0", 2, 0), wasmtest.ExportEntry("m1", 2, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(append(run, 0x0b)), wasmtest.Code(append(load, 0x0b)), wasmtest.Code([]byte{0x20, 0, 0x40, 1, 0x0b}), wasmtest.Code(append(store, 0x0b)), wasmtest.Code([]byte{0x3f, 1, 0x0b}))),
	)
}

func TestRV64PublicMultiMemoryNativeForeignParkedReentry(t *testing.T) {
	for _, swar := range []bool{false, true} {
		for _, artifact := range []bool{false, true} {
			t.Run(fmt.Sprintf("swar%t-artifact%t", swar, artifact), func(t *testing.T) {
				prefix := fmt.Sprintf("multimemory-calls-swar%t", swar)
				pc := rv64TableStageCompile(t, prefix+"-producer", rv64MMCallModule(true, swar), artifact)
				cc := rv64TableStageCompile(t, prefix+"-consumer", rv64MMCallModule(false, swar), artifact)
				borrowerSource := rv64MMModule(rv64MMGrowFuncs(1), nil, [][]byte{rv64MMImport("a", 1, 3), rv64MMImport("b", 1, 3)}, 2, nil, swar)
				bc := rv64TableStageCompile(t, prefix+"-borrower", borrowerSource, artifact)
				entered, release := make(chan struct{}), make(chan struct{})
				var releaseOnce sync.Once
				unblock := func() { releaseOnce.Do(func() { close(release) }) }
				defer unblock()
				var consumer *Instance
				calls := 0
				host := CallerHostCallFunc(func(caller Caller, call HostCall) {
					calls++
					if bytes := caller.Memory(); len(bytes) != 65536 || bytes[0] != 22 {
						panic("foreign host received consumer primary memory")
					}
					if calls == 1 {
						// Reentry switches to the original consumer's distinct memory directory.
						got, err := consumer.InvokeFromHost(context.Background(), caller, "grow", 0xdeadbeef00000001)
						if err != nil || !slices.Equal(got, []uint64{1}) {
							panic(fmt.Sprintf("secondary reentry growth: %v %v", got, err))
						}
						got, err = consumer.InvokeFromHost(context.Background(), caller, "store", 0xaabbccdd00010000, 0xdeadbeef000002bc)
						if err != nil || len(got) != 0 {
							panic(fmt.Sprintf("secondary reentry write: %v %v", got, err))
						}
						close(entered)
						<-release
					}
					call.SetI32(0, call.I32(0))
				})
				producer := rv64MMInstance(t, pc, NewImports().Function("env", "host", host))
				export, err := producer.ExportedFunc("produce")
				if err != nil {
					t.Fatal(err)
				}
				consumer = rv64MMInstance(t, cc, NewImports().Function("env", "host", export))
				rv64MMBytes(t, producer, 0)[0] = 22
				rv64MMBytes(t, consumer, 0)[0] = 11
				secondary, err := producer.ExportedMemory("m1")
				if err != nil {
					t.Fatal(err)
				}
				primary, err := NewSharedMemory(1, 3)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = primary.Close() })
				borrower := rv64MMInstance(t, bc, NewImports().Memory("env", "a", primary).Memory("env", "b", secondary))
				if producer.jm == consumer.jm || producer.nativeContext == consumer.nativeContext || borrower.jm == producer.jm {
					t.Fatal("call fixture did not create distinct execution homes")
				}
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				type result struct {
					values []uint64
					err    error
				}
				done, joined := make(chan result, 1), make(chan struct{})
				go func() {
					defer close(joined)
					values, err := consumer.InvokeContext(ctx, "call", 0xdeadbeef00010000)
					done <- result{slices.Clone(values), err}
				}()
				defer func() {
					cancel()
					unblock()
					select {
					case <-joined:
					case <-time.After(5 * time.Second):
						t.Error("multi-memory parked activation cleanup exceeded timeout")
						// Keep owners alive until the native worker exits; the
						// process test timeout diagnoses a bridge hang.
						<-joined
					}
				}()
				select {
				case <-entered:
				case got := <-done:
					t.Fatalf("foreign callback did not park: %v %v", got.values, got.err)
				case <-time.After(5 * time.Second):
					t.Fatal("foreign callback park timeout")
				}
				// The outer producer directory was built before this growth. Resume must
				// observe the selected JobMemory's authoritative size, not a cached row.
				for _, operation := range []struct {
					name       string
					args, want []uint64
				}{
					{"grow", []uint64{0xaabbccdd00000001}, []uint64{1}},
					{"store", []uint64{0xfeedface00010000, 300}, nil},
				} {
					got, err := borrower.InvokeContext(ctx, operation.name, operation.args...)
					if err != nil || !slices.Equal(got, operation.want) {
						t.Fatal("secondary access while foreign callback parked", operation.name, got, err)
					}
				}
				unblock()
				select {
				case got := <-done:
					if got.err != nil || !slices.Equal(got.values, []uint64{1000}) {
						t.Fatal("foreign/Go resume selected wrong memory directory", got.values, got.err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("multi-memory foreign resume timeout")
				}
				if rv64MMBytes(t, producer, 0)[0] != 22 || rv64MMBytes(t, consumer, 0)[0] != 11 || primary.UnsafeBytes()[0] != 0 {
					t.Fatal("foreign growth/write changed a primary memory")
				}
				rv64MMCall(t, producer, "size", []uint64{2})
				rv64MMCall(t, consumer, "size", []uint64{2})
				checkTrap := func(wantName string, wantPC uint32) {
					t.Helper()
					got, err := consumer.Invoke("call", 0xdeadbeef00020000)
					var trap *coreruntime.TrapError
					if got != nil || !errors.As(err, &trap) || trap.Code != coreruntime.TrapLinMemOutOfBounds || len(trap.Frames) != 1 || trap.Frames[0].FunctionIndex != 1 || trap.Frames[0].FunctionName != wantName || !trap.Frames[0].HasProgramCounter || trap.Frames[0].ProgramCounter != wantPC {
						t.Fatalf("%s indexed trap attribution: %v / %v", wantName, got, err)
					}
				}
				checkTrap("produce", 8)
				// Make that address valid only in the foreign producer. The next trap must
				// belong to the consumer's post-call load after restoring its own directory.
				rv64MMCall(t, borrower, "grow", []uint64{2}, 1)
				rv64MMCall(t, borrower, "store", nil, 2*65536, 400)
				checkTrap("call", 7)
				rv64MMCall(t, producer, "load", []uint64{300}, 65536)
				rv64MMCall(t, consumer, "load", []uint64{700}, 65536)
				rv64MMCall(t, consumer, "call", []uint64{1000}, 65536)
				if calls != 4 {
					t.Fatal("foreign callback count", calls)
				}
			})
		}
	}
}
