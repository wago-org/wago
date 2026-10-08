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
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

func rv64InvocationViewForTest(in *Instance) []byte {
	return unsafe.Slice((*byte)(offHeapPtr(railshotParkedNativeInvocation(in))), abi.RV64InvocationViewBytes)
}

func TestRV64PublicNativeInvocationSurvivesForeignHostReentry(t *testing.T) {
	for _, tableCall := range []bool{false, true} {
		for _, shared := range []bool{false, true} {
			for _, artifact := range []bool{false, true} {
				t.Run(fmt.Sprintf("table%t-shared%t-artifact%t", tableCall, shared, artifact), func(t *testing.T) {
					var memory *Memory
					if shared {
						var err error
						memory, err = NewSharedMemory(1, 4)
						if err != nil {
							t.Fatal(err)
						}
						defer memory.Close()
					}
					var producer, consumer *Instance
					depth := 0
					calls := 0
					nestedDone := false
					fail := false
					stopped := errors.New("invocation host stop")
					host := CallerHostCallFunc(func(caller Caller, call HostCall) {
						calls++
						root := consumer
						if depth != 0 {
							root = producer
						}
						expected := root.eng.RV64InvocationViewPointer()
						pointer := railshotParkedNativeInvocation(producer)
						if depth != 0 && pointer != expected || depth == 0 && (pointer < root.eng.StackLimit() || pointer+abi.RV64InvocationViewBytes > expected) {
							panic("foreign selector lost actual activation view")
						}
						view := rv64InvocationViewForTest(producer)
						if binary.LittleEndian.Uint64(view) != abi.RV64InvocationMagic || binary.LittleEndian.Uint64(view[8:]) != uint64(root.eng.StackLimit()) || binary.LittleEndian.Uint64(view[16:]) != uint64(expected) || binary.LittleEndian.Uint64(view[56:]) == 0 {
							panic("invocation actual extents")
						}
						args, results := offHeapSlicePtr(root.serArgs), offHeapSlicePtr(root.results)
						argsBytes, resultsBytes := len(root.serArgs), len(root.results)
						if depth == 0 {
							args, results = pointer-4096, pointer-2048
							argsBytes, resultsBytes = 2048, 2048
						}
						if binary.LittleEndian.Uint64(view[24:]) != uint64(args) || binary.LittleEndian.Uint64(view[32:]) != uint64(argsBytes) || binary.LittleEndian.Uint64(view[40:]) != uint64(results) || binary.LittleEndian.Uint64(view[48:]) != uint64(resultsBytes) {
							panic("actual activation transport capacities")
						}
						if depth == 0 && !nestedDone {
							nestedDone = true
							before := bytes.Clone(view)
							outerEngine := producer.eng
							depth++
							got, err := producer.InvokeFromHost(context.Background(), caller, "producer", 8)
							depth--
							if err != nil || !slices.Equal(got, []uint64{9}) || producer.eng != outerEngine || !bytes.Equal(view, before) {
								panic(fmt.Sprintf("producer reentry got=%v err=%v engine=%t pointer=%x want=%x view=%t", got, err, producer.eng == outerEngine, railshotSaveNativeInvocation(producer), expected, bytes.Equal(view, before)))
							}
							safeArgs, safeWant := []uint64{51}, []uint64{51}
							if tableCall {
								safeArgs, safeWant = nil, []uint64{7}
							}
							if got, err := consumer.InvokeFromHost(context.Background(), caller, "safe", safeArgs...); err != nil || !slices.Equal(got, safeWant) || railshotSaveNativeInvocation(consumer) != expected || !bytes.Equal(view, before) {
								panic("root reentry overwrote parked view")
							}
							runtime.GC()
							if fail {
								panic(HostTrap{Err: stopped})
							}
						}
						call.SetI32(0, call.I32(0)+1)
					})
					imports := testImports("env.host", host)
					if shared {
						imports.Memory("env", "memory", memory)
					}
					var err error
					producerTail := []byte{0x20, 0, 0x10, 0, 0x1a}
					if tableCall {
						// A real ref.func use requests the lazy public descriptor storage.
						producerTail = append(producerTail, 0xd2, 1, 0x1a)
					}
					producerSource := rv64PublicNativeHostModule(shared, "producer", producerTail)
					producer, err = Instantiate(rv64TableStageCompile(t, fmt.Sprintf("invocation-producer-table%t-shared%t", tableCall, shared), producerSource, artifact), imports)
					if err != nil {
						t.Fatal(err)
					}
					defer producer.Close()
					export, err := producer.ExportedFunc("producer")
					if err != nil {
						t.Fatal(err)
					}
					imports = testImports("env.host", export)
					if tableCall {
						imports = NewImports()
					}
					if shared {
						imports.Memory("env", "memory", memory)
					}
					consumerSource := rv64PublicNativeHostModule(shared, "consumer", nil)
					if tableCall {
						consumerSource = rv64ForeignTableCallModule([]wasm.ValType{wasm.I32}, 7, shared, false)
					}
					consumer, err = Instantiate(rv64TableStageCompile(t, fmt.Sprintf("invocation-consumer-table%t-shared%t", tableCall, shared), consumerSource, artifact), imports)
					if err != nil {
						t.Fatal(err)
					}
					defer consumer.Close()
					if tableCall {
						rv64InstallForeignTableTargetAt(t, consumer, producer, 0)
					}
					name, input, want := "consumer", []uint64{7}, []uint64{8}
					if tableCall {
						name, input, want = "call", []uint64{7, 0}, []uint64{8, 7}
					}
					for range 2 {
						nestedDone = false
						if got, err := consumer.Invoke(name, input...); err != nil || !slices.Equal(got, want) {
							t.Fatal("outer invocation", got, err)
						}
						fail = true
						nestedDone = false
						if got, err := consumer.Invoke(name, input...); got != nil || !errors.Is(err, stopped) {
							t.Fatal("host cold unwind", got, err)
						}
						fail = false
					}
					if calls != 14 {
						t.Fatal("nested host witness did not execute", calls)
					}
					fn, err := consumer.WasmFunc("safe")
					if err != nil {
						t.Fatal(err)
					}
					safeArgs, safeWant := []uint64{73}, []uint64{73}
					if tableCall {
						safeArgs, safeWant = nil, []uint64{7}
					}
					if got, err := fn.Invoke(safeArgs...); err != nil || !slices.Equal(got, safeWant) || railshotSaveNativeInvocation(consumer) != consumer.eng.RV64InvocationViewPointer() {
						t.Fatal("prepared entry after cold exit", got, err)
					}
				})
			}
		}
	}

}

// Optimized typed and fixed-view host dispatch must restore its root view before
// a local prologue or second import executes after a shared-memory guest entry.
func TestRV64PublicNativeInvocationTypedSharedReentry(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, fixedView := range []bool{false, true} {
			t.Run(fmt.Sprintf("artifact%t-fixedView%t", artifact, fixedView), func(t *testing.T) {
				memory, err := NewSharedMemory(1, 4)
				if err != nil {
					t.Fatal(err)
				}
				defer memory.Close()
				other, err := Instantiate(rv64TableStageCompile(t, "invocation-typed-other", rv64PublicNativeHostModule(true, "other", nil), artifact), NewImports().Memory("env", "memory", memory).Function("env", "host", func(v int32) int32 { return v }))
				if err != nil {
					t.Fatal(err)
				}
				defer other.Close()
				calls := 0
				invoke := func(v int32) int32 {
					calls++
					if got, err := other.Invoke("safe", uint64(uint32(v))); err != nil || !slices.Equal(got, []uint64{uint64(uint32(v))}) {
						panic(fmt.Sprintf("shared typed reentry: %v %v", got, err))
					}
					return v + 1
				}
				var host any = invoke
				if fixedView {
					host = HostCallFunc(func(call HostCall) { call.SetI32(0, invoke(call.I32(0))) })
				}
				// The first host call leaves its result on stack; invoke the local safe
				// function and host import again before returning that original result.
				source := rv64PublicNativeHostModule(true, "root", []byte{0x20, 0, 0x10, 2, 0x1a, 0x20, 0, 0x10, 0, 0x1a})
				root, err := Instantiate(rv64TableStageCompile(t, "invocation-typed-root", source, artifact), NewImports().Memory("env", "memory", memory).Function("env", "host", host))
				if err != nil {
					t.Fatal(err)
				}
				defer root.Close()
				for range 3 {
					if got, err := root.Invoke("root", 7); err != nil || !slices.Equal(got, []uint64{8}) {
						t.Fatal("typed root after reentry", got, err)
					}
				}
				if calls != 6 {
					t.Fatal("typed callback witness", calls)
				}
			})
		}
	}
}
