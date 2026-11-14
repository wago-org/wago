//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Keep the writer in a callee and the reader/writer in a loop so trace is a
// module-wide value pin. A helper error must publish the current register,
// including callee writes, before it leaves the entire native activation.
func gcHelperGlobalErrorModule(wide, allocation, allPins bool) []byte {
	valueType := wasm.I32
	constant, mul, add := byte(0x41), byte(0x6c), byte(0x6a)
	if wide {
		valueType = wasm.I64
		constant, mul, add = 0x42, 0x7e, 0x7c
	}
	seq := func(parts ...[]byte) []byte {
		var b []byte
		for _, part := range parts {
			b = append(b, part...)
		}
		return b
	}
	event := func(n byte) []byte {
		b := []byte{0x23, 0, constant, 10, mul, constant, n, add, 0x24, 0}
		if allPins {
			b = append(b, 0x23, 3, 0x41, 10, 0x6c, 0x41, n, 0x6a, 0x24, 3, 0x23, 4, 0x42, 10, 0x7e, 0x42, n, 0x7c, 0x24, 4)
		}
		return b
	}
	initial := uint64(0)
	if wide {
		initial = 0x100000002
	}
	expr := []byte{constant}
	value := initial
	for {
		b := byte(value & 127)
		value >>= 7
		if value != 0 || b&64 != 0 {
			expr = append(expr, b|128)
		} else {
			expr = append(expr, b)
			break
		}
	}
	expr = append(expr, 0xb)
	global := seq([]byte{wasm.MustEncodeValType(valueType), 1}, expr)
	// The initial object/array enables the generic collector boundary.
	owner := []byte{0x64, 0, 0, 0x41, 42, 0xfb, 0, 0, 0xb}
	if allocation {
		owner = []byte{0x64, 3, 0, 0x41, 8, 0xfb, 7, 3, 0xb}
	}
	globalsList := [][]byte{global, owner, {0x7f, 1, 0x41, 0, 0xb}}
	if allPins {
		globalsList = append(globalsList, []byte{0x7f, 1, 0x41, 13, 0xb}, []byte{0x7e, 1, 0x42, 14, 0xb})
	}
	globals := wasmtest.Vec(globalsList...)
	repeats := 1
	if allPins {
		repeats = 100
	}
	var consumer []byte
	for i := 0; i < repeats; i++ {
		consumer = append(consumer, event(3)...)
	}
	writer := seq(event(1), []byte{0x41, 1, 0xb})
	payload := seq(event(2), []byte{0xd0, 0x6d, 0xb})
	run := seq([]byte{2, 0x40, 3, 0x40, 0x10, 0, 4, 0x40, 0x10, 1, 0xfb, 22, 0x6c, 0xfb, 30, 0x1a}, consumer, []byte{0xc, 1, 0xb, 0xc, 1, 0xb, 0xb, 0x41, 0, 0xb})
	if allocation {
		// Retain each constructed object. Tiny has room for the array and exactly
		// three 24-byte objects, so the fourth constructor fails after the writer.
		run = seq([]byte{2, 0x40, 3, 0x40, 0x10, 0, 0x1a, 0x23, 1, 0x20, 0, 0x41, 42, 0xfb, 0, 0, 0xfb, 14, 3}, consumer, []byte{0x20, 0, 0x41, 1, 0x6a, 0x22, 0, 0x24, 2, 0x20, 0, 0x41, 8, 0x49, 0xd, 0, 0xb, 0xb, 0x41, 0, 0xb})
	}
	body := wasmtest.Code(run)
	if allocation {
		raw := append([]byte{1, 1, 0x7f}, run...)
		body = append(wasmtest.ULEB(uint32(len(raw))), raw...)
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 0}, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), wasmtest.FuncType(nil, []wasm.ValType{wasm.EqRef}), []byte{0x5e, 0x6d, 1})),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(1))),
		wasmtest.Section(6, globals),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 2))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(writer), wasmtest.Code(payload), body)),
	)
}

func TestGCInternalHelperErrorsPublishModuleGlobals(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, allocation := range []bool{false, true} {
			for _, allPins := range []bool{false, true} {
				for _, native := range []bool{false, true} {
					t.Run(fmt.Sprintf("i64=%t/allocation=%t/pins3=%t/native=%t", wide, allocation, allPins, native), func(t *testing.T) {
						cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompileWorkers(1).WithOptimization("inline", false).WithOptimization("gc-native-alloc", native)
						c, e := Compile(cfg, gcHelperGlobalErrorModule(wide, allocation, allPins))
						if e != nil {
							t.Fatal(e)
						}
						defer c.Close()
						gcCfg := GCConfig{NurseryBytes: 1 << 20, ThroughputHeapBytes: 16 << 20}
						if allocation {
							gcCfg = GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 128, TinyBlockBytes: 8}
						}
						in, e := Instantiate(c, InstantiateOptions{GC: gcCfg})
						if e != nil {
							t.Fatal(e)
						}
						defer func() {
							if e := in.Close(); e != nil {
								t.Error(e)
							}
							if e := in.WaitClosed(context.Background()); e != nil {
								t.Error(e)
							}
							if !in.resourcesClosed || in.base != 0 {
								t.Error("instance resources not released")
							}
						}()
						before := in.gc.Stats()
						_, e = in.Invoke("run")
						if e == nil {
							t.Fatal("expected helper failure")
						}
						after := in.gc.Stats()
						initial := uint64(0)
						if wide {
							initial = 0x100000002
						}
						want := initial*100 + 12
						if allocation {
							if !strings.Contains(e.Error(), "tiny heap exhausted") {
								t.Fatalf("wrong allocation error: %v", e)
							}
							if got := in.globalCells[2].Get(); got != 3 {
								t.Fatalf("completed objects%d want3", got)
							}
							if got := after.Allocations - before.Allocations; got != 3 {
								t.Fatalf("successful allocations%d want3", got)
							}
							want = initial
							for i := 0; i < 3; i++ {
								want = want*10 + 1
								repeats := 1
								if allPins {
									repeats = 100
								}
								for j := 0; j < repeats; j++ {
									want = want*10 + 3
								}
							}
							want = want*10 + 1
							if !wide {
								want = uint64(uint32(want))
							}
						} else {
							var trap *coreruntime.TrapError
							if !errors.As(e, &trap) || trap.Code != coreruntime.TrapCastFailure {
								t.Fatalf("wrong cast error: %v", e)
							}
							if after.Allocations != before.Allocations {
								t.Fatal("cast unexpectedly allocated")
							}
						}
						got := in.globalCells[0].Get()
						t.Logf("trace%d want%d failure%v", got, want, e)
						if got != want {
							t.Fatalf("helper error lost completed module-global writes: got%d want%d", got, want)
						}
						if allPins {
							for _, x := range []struct {
								global  int
								initial uint64
								wide    bool
							}{{3, 13, false}, {4, 14, true}} {
								v := x.initial
								if allocation {
									for i := 0; i < 3; i++ {
										v = v*10 + 1
										for j := 0; j < 100; j++ {
											v = v*10 + 3
										}
									}
									v = v*10 + 1
								} else {
									v = v*100 + 12
								}
								if !x.wide {
									v = uint64(uint32(v))
								}
								if got := in.globalCells[x.global].Get(); got != v {
									t.Fatalf("helper error lost global%d: got%d want%d", x.global, got, v)
								}
							}
						}

					})
				}
			}
		}
	}
}
