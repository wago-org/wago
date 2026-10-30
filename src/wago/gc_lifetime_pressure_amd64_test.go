//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Exercise the admitted 32-field boundary with mixed GP/FP arguments across
// the allocation helper. Parameters must remain valid after its clobbers and
// nursery refill, including exact floating-point payload bits.
func TestGCLifetimeStage2MixedFieldPressure(t *testing.T) {
	types := []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64}
	tags := []byte{0x7f, 0x7e, 0x7d, 0x7c}
	params := make([]wasm.ValType, 32)
	structType := []byte{0x5f, 32}
	for j := range params {
		params[j] = types[j%4]
		structType = append(structType, tags[j%4], 0)
	}
	for _, field := range []int{0, 1, 2, 3, 28, 29, 30, 31} {
		body := []byte{1, 1, 0x63, 0} // one nullable reference local, after 32 parameters
		for j := range params {
			body = append(body, 0x20, byte(j))
		}
		body = append(body, 0xfb, 0, 0, 0x21, 32, 0x20, 32, 0xfb, 2, 0, byte(field), 0x0b)
		module := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(structType, wasmtest.FuncType(params, []wasm.ValType{params[field]}))),
			wasmtest.Section(3, wasmtest.Vec([]byte{1})),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))))
		for _, on := range []bool{false, true} {
			t.Run(fmt.Sprintf("field=%d/on=%v", field, on), func(t *testing.T) {
				i := lifetimeInstance(t, module, on, GCConfig{NurseryBytes: 1024, SurvivorBytes: 1024, VerifyAfterCollect: true})
				var handleless uint64
				for n := uint64(0); n < 96; n++ {
					values := make([]uint64, len(params))
					for j := range values {
						switch j % 4 {
						case 0:
							values[j] = uint64(uint32(0x80000000 + n + uint64(j)))
						case 1:
							values[j] = 0x8000000000000000 + n + uint64(j)
						case 2:
							values[j] = 0xffc00000 + n + uint64(j)
						case 3:
							values[j] = 0xfff8000000000000 + n + uint64(j)
						}
					}
					before := lifetimeSnapshot(i)
					got, err := i.Invoke("run", values...)
					after := lifetimeSnapshot(i)
					handleless += lifetimeHandleless(before, after)
					if err != nil || len(got) != 1 || got[0] != values[field] {
						t.Fatalf("iteration=%d got=%x err=%v want=%x", n, got, err, values[field])
					}
				}
				if on && gc.NativeDeadBatchSupported && nativeStructAllocEnabled && handleless == 0 {
					t.Fatal("test did not exercise the handleless path")
				}
			})
		}
	}
}

func TestGCLifetimeStage2CallerFrameRoot(t *testing.T) {
	callee := []byte{1, 2, 0x63, 0, 0x41, 42, 0xfb, 0, 0, 0x21, 0, 0x20, 0, 0x22, 1, 0x1a, 0x01, 0x20, 1, 0xfb, 2, 0, 0, 0x0b}
	// Keep a separate live object in the caller while repeatedly calling the
	// qualifying callee. Disable inlining so collection must walk both frames.
	caller := []byte{2, 1, 0x63, 0, 1, 0x7f,
		0x41, 7, 0xfb, 0, 0, 0x21, 0,
		0x41, 0, 0x21, 1, 0x03, 0x40,
		0x10, 0, 0x1a,
		0x20, 1, 0x41, 1, 0x6a, 0x22, 1, 0x41, 32, 0x49, 0x0d, 0,
		0x0b, 0x20, 0, 0xfb, 2, 0, 0, 0x0b}
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 0}, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(callee))), callee...), append(wasmtest.ULEB(uint32(len(caller))), caller...))))
	for _, on := range []bool{false, true} {
		t.Run(fmt.Sprintf("on=%v", on), func(t *testing.T) {
			c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("inline", false).WithOptimization("gc-lifetime-analyze", on), module)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { c.Close() })
			i, err := Instantiate(c, InstantiateOptions{GC: GCConfig{NurseryBytes: 1024, SurvivorBytes: 1024, VerifyAfterCollect: true}})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { i.Close() })
			var handleless uint64
			for n := 0; n < 32; n++ {
				a := lifetimeSnapshot(i)
				got, err := i.Invoke("run")
				b := lifetimeSnapshot(i)
				handleless += lifetimeHandleless(a, b)
				if err != nil || len(got) != 1 || got[0] != 7 {
					t.Fatal(got, err)
				}
			}
			if i.gc.Stats().MinorCollections == 0 {
				t.Fatal("caller-root test did not collect")
			}
			if on && gc.NativeDeadBatchSupported && nativeStructAllocEnabled && handleless == 0 {
				t.Fatal("callee did not exercise handleless path")
			}
		})
	}
}
