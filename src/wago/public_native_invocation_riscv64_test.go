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

	"github.com/wago-org/wago/src/core/runtime/abi"
)

func rv64InvocationViewForTest(in *Instance) []byte {
	return unsafe.Slice((*byte)(offHeapPtr(railshotSaveNativeInvocation(in))), abi.RV64InvocationViewBytes)
}

func TestRV64PublicNativeInvocationSurvivesForeignHostReentry(t *testing.T) {
	for _, shared := range []bool{false, true} {
		for _, artifact := range []bool{false, true} {
			t.Run(fmt.Sprintf("shared%t-artifact%t", shared, artifact), func(t *testing.T) {
				var memory *Memory
				if shared {
					_, memory = importedMemoryProvider(t)
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
					if railshotSaveNativeInvocation(producer) != expected {
						panic("foreign selector lost root invocation")
					}
					view := rv64InvocationViewForTest(producer)
					if binary.LittleEndian.Uint64(view) != abi.RV64InvocationMagic || binary.LittleEndian.Uint64(view[8:]) != uint64(root.eng.StackLimit()) || binary.LittleEndian.Uint64(view[16:]) != uint64(expected) || binary.LittleEndian.Uint64(view[24:]) != uint64(offHeapSlicePtr(root.serArgs)) || binary.LittleEndian.Uint64(view[32:]) != uint64(len(root.serArgs)) || binary.LittleEndian.Uint64(view[40:]) != uint64(offHeapSlicePtr(root.results)) || binary.LittleEndian.Uint64(view[48:]) != uint64(len(root.results)) {
						panic("invocation actual extents")
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
						if got, err := consumer.InvokeFromHost(context.Background(), caller, "safe", 51); err != nil || !slices.Equal(got, []uint64{51}) || railshotSaveNativeInvocation(consumer) != expected || !bytes.Equal(view, before) {
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
				producer, err = Instantiate(rv64PublicStartCompile(t, rv64PublicNativeHostModule(shared, "producer", []byte{0x20, 0, 0x10, 0, 0x1a}), artifact), imports)
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
				consumer, err = Instantiate(rv64PublicStartCompile(t, rv64PublicNativeHostModule(shared, "consumer", nil), artifact), imports)
				if err != nil {
					t.Fatal(err)
				}
				defer consumer.Close()
				for range 2 {
					nestedDone = false
					if got, err := consumer.Invoke("consumer", 7); err != nil || !slices.Equal(got, []uint64{8}) {
						t.Fatal("outer invocation", got, err)
					}
					fail = true
					nestedDone = false
					if got, err := consumer.Invoke("consumer", 7); got != nil || !errors.Is(err, stopped) {
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
				if got, err := fn.Invoke(73); err != nil || !slices.Equal(got, []uint64{73}) || railshotSaveNativeInvocation(consumer) != consumer.eng.RV64InvocationViewPointer() {
					t.Fatal("prepared entry after cold exit", got, err)
				}
			})
		}
	}
}
