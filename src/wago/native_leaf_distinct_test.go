//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package wago

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	wruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// The alias is deliberately separate from the distinct unary and binary leaves.
func distinctLeafModule(order []string) []byte {
	indexes := map[string]byte{}
	imports := make([][]byte, len(order))
	for i, name := range order {
		indexes[name] = byte(i)
		typ := byte(0)
		if name == "pair" {
			typ = 1
		}
		imports[i] = importEntry("env", name, 0, uint32(typ))
	}
	a, b, pair, alias := indexes["a"], indexes["b"], indexes["pair"], indexes["alias"]
	code := [][]byte{
		// Keep a local and mutable global live across three distinct imports.
		{1, 1, 0x7f, 0x20, 0, 0x41, 17, 0x6a, 0x21, 2, 0x20, 0, 0x10, a, 0x10, b, 0x20, 1, 0x10, pair, 0x20, 2, 0x6a, 0x23, 0, 0x6a, 0x21, 2, 0x23, 0, 0x41, 1, 0x6a, 0x24, 0, 0x20, 2, 0x0b},
		{0, 0x20, 0, 0x41, 0, 0x11, 0, 0, 0x41, 1, 0x11, 0, 0, 0x0b},
		{0, 0x20, 0, 0xd2, b, 0x14, 0, 0x0b},
		{0, 0x20, 0, 0x20, 1, 0x12, pair, 0x0b},
		{0, 0x20, 0, 0x10, alias, 0x1a, 0, 0x0b},
	}
	for i, body := range code {
		code[i] = append(wasmtest.ULEB(uint32(len(body))), body...)
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(imports...)),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{0}, []byte{0}, []byte{1}, []byte{0})),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0, 2})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x7f, 1, 0x41, 7, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 4), wasmtest.ExportEntry("table", 0, 5), wasmtest.ExportEntry("ref", 0, 6), wasmtest.ExportEntry("tail", 0, 7), wasmtest.ExportEntry("trap", 0, 8), wasmtest.ExportEntry("reexport", 0, uint32(a)))),
		wasmtest.Section(9, wasmtest.Vec([]byte{0, 0x41, 0, 0x0b, 2, a, b})),
		wasmtest.Section(10, wasmtest.Vec(code...)))
}

func distinctLeafCells(in *Instance) []byte {
	ptr := in.jm.CaptureInstanceContext().ImportDispatch
	if ptr == 0 {
		return nil
	}
	return unsafe.Slice((*byte)(offHeapPtr(ptr)), len(in.syncHosts)*wruntime.ImportDispatchEntryBytes)
}

func assertDistinctLeafRoute(t *testing.T, in *Instance, want bool) {
	t.Helper()
	if got := in.executionFlags.Load()&executionFlagNativeScalarLeaf != 0; got != want {
		t.Fatalf("native flag=%t want=%t", got, want)
	}
	cells := distinctLeafCells(in)
	if len(cells) == 0 {
		t.Fatal("missing dispatch array")
	}
	for i := range in.syncHosts {
		cell := cells[i*wruntime.ImportDispatchEntryBytes:]
		if got := binary.LittleEndian.Uint64(cell[wruntime.ImportDispatchCallerContextOffset:])&wruntime.ImportDispatchCallerNativeLeafTag != 0; got != want {
			t.Fatalf("import %d native tag=%t want=%t", i, got, want)
		}
	}
	if want && (in.boundedHostSegments() || in.isolatedNativeScalarLeaf()) {
		t.Fatal("distinct leaves broadened bounded/isolated entry")
	}
}

func distinctLeafImports() *Imports {
	return testImports("env.a", nativeLeafIncrement, "env.b", nativeLeafIncrementFive, "env.pair", nativeLeafPolynomial, "env.alias", nativeLeafIncrement)
}

func TestNativeLeafDistinctDifferentialRoutes(t *testing.T) {
	for _, order := range [][]string{{"a", "b", "pair", "alias"}, {"pair", "alias", "b", "a"}, {"b", "pair", "a", "alias"}} {
		t.Run(fmt.Sprint(order), func(t *testing.T) {
			module := distinctLeafModule(order)
			instances := make([]*Instance, 2)
			for i, enabled := range []bool{false, true} {
				c, err := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithBoundsChecks(BoundsChecksExplicit).WithOptimization("native-leaf-host", enabled).Compile(module)
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				instances[i], err = Instantiate(c, InstantiateOptions{Imports: distinctLeafImports()})
				if err != nil {
					t.Fatal(err)
				}
				defer instances[i].Close()
				assertDistinctLeafRoute(t, instances[i], enabled && !codeProfileEnabled)
			}
			for _, name := range []string{"run", "table", "run", "ref", "run", "tail", "reexport", "run", "trap", "run"} {
				for _, x := range []uint64{0, 0xffffffff, 0x80000000, 0x7fffffff, 0xdeadbeef00000007, 0xffffffffffffffff} {
					args := []uint64{x}
					if name == "run" || name == "tail" {
						args = append(args, 0xaabbccddfffffffd)
					}
					// Direct entries bypass control-frame staging. Poison the
					// packed import/shape word to prove native execution, while
					// the independently forced generic oracle must stage it.
					const marker = uint64(0xfefdfcfbfaf9f8f7)
					if name == "run" {
						for _, in := range instances {
							binary.LittleEndian.PutUint64(in.ctrl[wruntime.HostCtrlImportIndexOffset:], marker)
						}
					}
					want, werr := instances[0].Invoke(name, args...)
					got, gerr := instances[1].Invoke(name, args...)
					if name == "run" {
						for index, in := range instances {
							unchanged := binary.LittleEndian.Uint64(in.ctrl[wruntime.HostCtrlImportIndexOffset:]) == marker
							if unchanged != (index == 1 && !codeProfileEnabled) {
								t.Fatalf("direct route staging unchanged=%t instance=%d", unchanged, index)
							}
						}
					}
					if !reflect.DeepEqual(got, want) || fmt.Sprint(gerr) != fmt.Sprint(werr) {
						t.Fatalf("%s(%x): native=%x,%v generic=%x,%v", name, args, got, gerr, want, werr)
					}
					for _, raw := range got {
						if raw>>32 != 0 {
							t.Fatalf("noncanonical i32 result %x", raw)
						}
					}
					if (name == "trap") != (gerr != nil) {
						t.Fatalf("unexpected trap=%v for %s", gerr, name)
					}
				}
			}
			// Checked foreign ownership reaches the provider's generic addressable thunk.
			for _, provider := range instances {
				ex, err := provider.ExportedFunc("table")
				if err != nil {
					t.Fatal(err)
				}
				c, err := NewRuntimeConfig().Compile(benchReturningImportModule())
				if err != nil {
					t.Fatal(err)
				}
				consumer, err := Instantiate(c, InstantiateOptions{Imports: testImports("env.f", ex)})
				if err != nil {
					c.Close()
					t.Fatal(err)
				}
				if consumer.executionFlags.Load()&executionFlagNativeScalarLeaf != 0 {
					t.Fatal("foreign binding admitted")
				}
				got, err := consumer.Invoke("g", 0xfeedfaceffffffff)
				if err != nil || len(got) != 1 || got[0] != 5 {
					t.Fatalf("foreign=%x,%v", got, err)
				}
				consumer.Close()
				c.Close()
			}
		})
	}
}

func TestNativeLeafDistinctPublicationRollback(t *testing.T) {
	if codeProfileEnabled {
		t.Skip("profiling retains generic route")
	}
	// Begin with real, generic dispatch cells, then re-register while unpublished.
	c, err := NewRuntimeConfig().Compile(nativeLeafAliasModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for fail := 0; fail <= 2; fail++ {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			in, err := Instantiate(c, InstantiateOptions{Imports: testImports("env.a", capturedNativeLeafAlias(1), "env.b", capturedNativeLeafAlias(5))})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			unregisterHostControl(in)
			in.syncHosts[0].typedI32 = nativeLeafIncrement
			in.syncHosts[1].typedI32 = nativeLeafIncrementFive
			original := append([]byte(nil), distinctLeafCells(in)...)
			ctrl := append([]byte(nil), in.ctrl...)
			injected := errors.New("injected publication failure")
			owner := func(*Instance) error {
				if fail == 2 {
					return injected
				}
				return nil
			}
			err = registerHostControlTransaction(in, owner, func(index int) error {
				if index == fail {
					return injected
				}
				return nil
			})
			if !errors.Is(err, injected) {
				t.Fatalf("failure=%v", err)
			}
			if !bytes.Equal(original, distinctLeafCells(in)) || !bytes.Equal(ctrl, in.ctrl) || in.executionFlags.Load()&executionFlagNativeScalarLeaf != 0 {
				t.Fatal("rollback did not restore cells/control/flags")
			}
			if _, ok := hostControlInstances.Load(offHeapSlicePtr(in.ctrl)); ok {
				t.Fatal("failed registration remains published")
			}
			if err = registerHostControl(in); err != nil {
				t.Fatal(err)
			}
			assertDistinctLeafRoute(t, in, true)
			got, err := in.Invoke("run", 7)
			if err != nil || got[0] != 13 {
				t.Fatalf("retry=%v,%v", got, err)
			}
		})
	}
}

func TestNativeLeafDistinctLoopScheduling(t *testing.T) {
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(importEntry("env", "a", 0, 0), importEntry("env", "b", 0, 0))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 2))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x03, 0x40, 0x20, 1, 0x10, 0, 0x10, 1, 0x21, 1, 0x20, 0, 0x41, 1, 0x6b, 0x22, 0, 0x0d, 0, 0x0b, 0x20, 1, 0x0b}))))
	testNativeLeafLoopScheduling(t, module, testImports("env.a", nativeLeafIncrement, "env.b", nativeLeafIncrementFive))
}

func TestNativeLeafDistinctMixedEffectsAndTraps(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		c, err := NewRuntimeConfig().WithOptimization("native-leaf-host", enabled).Compile(nativeLeafAliasModule())
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		calls := 0
		trap := false
		callback := func(v int32) int32 {
			calls++
			if trap {
				panic(HostTrap{Err: errors.New("distinct callback trap")})
			}
			return v + 5
		}
		in, err := Instantiate(c, InstantiateOptions{Imports: testImports("env.a", nativeLeafIncrement, "env.b", callback)})
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		assertDistinctLeafRoute(t, in, false)
		got, err := in.Invoke("run", 0xabcdef00ffffffff)
		if err != nil || got[0] != 5 || calls != 1 {
			t.Fatalf("effect=%v,%v calls=%d", got, err, calls)
		}
		trap = true
		if _, err = in.Invoke("run", 7); err == nil || calls != 2 {
			t.Fatalf("trap=%v calls=%d", err, calls)
		}
		trap = false
		got, err = in.Invoke("run", 7)
		if err != nil || got[0] != 13 || calls != 3 {
			t.Fatalf("recovery=%v,%v calls=%d", got, err, calls)
		}
	}
}

func TestNativeLeafDistinctDomainAndGateFallback(t *testing.T) {
	c, err := NewRuntimeConfig().Compile(nativeLeafAliasModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, flag := range []uint32{executionFlagImportedGCDomain, executionFlagDynamicGCDomain, executionFlagStoreOwnedGCCollector} {
		in, err := Instantiate(c, InstantiateOptions{Imports: testImports("env.a", capturedNativeLeafAlias(1), "env.b", capturedNativeLeafAlias(5))})
		if err != nil {
			t.Fatal(err)
		}
		unregisterHostControl(in)
		in.syncHosts[0].typedI32 = nativeLeafIncrement
		in.syncHosts[1].typedI32 = nativeLeafIncrementFive
		original := in.executionFlags.Load()
		in.executionFlags.Store(original | flag)
		if err = registerHostControl(in); err != nil {
			t.Fatal(err)
		}
		assertDistinctLeafRoute(t, in, false)
		in.executionFlags.Store(original)
		in.Close()
	}
	gate := newPluginCallGate("distinct-test")
	in, err := Instantiate(c, InstantiateOptions{Imports: testImports("env.a", nativeLeafIncrement, "env.b", gatedI32ToI32HostFunc{fn: nativeLeafIncrementFive, gate: gate})})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	assertDistinctLeafRoute(t, in, false)
	got, err := in.Invoke("run", 7)
	if err != nil || got[0] != 13 {
		t.Fatalf("gated=%v,%v", got, err)
	}
	gate.deactivate()
	if _, err = in.Invoke("run", 7); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("revoked gate=%v", err)
	}
}

func TestNativeLeafDistinctCodeSharingClose(t *testing.T) {
	if codeProfileEnabled {
		t.Skip("profiling retains generic route")
	}
	c, err := NewRuntimeConfig().Compile(nativeLeafAliasModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	instances := make([]*Instance, 2)
	for i := range instances {
		instances[i], err = Instantiate(c, InstantiateOptions{Imports: testImports("env.a", nativeLeafIncrement, "env.b", nativeLeafIncrementFive)})
		if err != nil {
			t.Fatal(err)
		}
		defer instances[i].Close()
		assertDistinctLeafRoute(t, instances[i], true)
	}
	for i := range instances[0].syncHosts {
		offset := i * wruntime.ImportDispatchEntryBytes
		if binary.LittleEndian.Uint64(distinctLeafCells(instances[0])[offset:]) != binary.LittleEndian.Uint64(distinctLeafCells(instances[1])[offset:]) {
			t.Fatal("distinct code not shared")
		}
	}
	instances[0].Close()
	for _, x := range []uint64{0, 0xffffffff, 0x80000000, 0xdeadbeef00000007} {
		got, err := instances[1].Invoke("run", x)
		if err != nil || got[0] != I32(int32(x)+6) {
			t.Fatalf("surviving instance=%x,%v", got, err)
		}
	}
}

func TestNativeLeafDistinctLocalGCFallback(t *testing.T) {
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), []byte{0x5f, 0})),
		wasmtest.Section(2, wasmtest.Vec(importEntry("env", "a", 0, 0), importEntry("env", "b", 0, 0))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 2))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xfb, 0, 1, 0x1a, 0x20, 0, 0x10, 0, 0x10, 1, 0x0b}))))
	c, err := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).Compile(module)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{Imports: testImports("env.a", nativeLeafIncrement, "env.b", nativeLeafIncrementFive)})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if in.gc == nil {
		t.Fatal("fixture did not acquire a local GC domain")
	}
	assertDistinctLeafRoute(t, in, false)
	got, err := in.Invoke("run", 7)
	if err != nil || got[0] != 13 {
		t.Fatalf("GC generic=%v,%v", got, err)
	}
}

//go:noinline
//go:norace
func nativeLeafDistinctXor(a, b int32) int32 { return a ^ b }

func TestNativeLeafDistinctBinaryDifferential(t *testing.T) {
	sig := wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32})
	module := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(sig)),
		wasmtest.Section(2, wasmtest.Vec(importEntry("env", "a", 0, 0), importEntry("env", "b", 0, 0))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 2))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x10, 0, 0x20, 1, 0x10, 1, 0x0b}))))
	for _, reverse := range []bool{false, true} {
		a, b := nativeLeafPolynomial, nativeLeafDistinctXor
		if reverse {
			a, b = b, a
		}
		for _, enabled := range []bool{false, true} {
			c, err := NewRuntimeConfig().WithOptimization("native-leaf-host", enabled).Compile(module)
			if err != nil {
				t.Fatal(err)
			}
			in, err := Instantiate(c, InstantiateOptions{Imports: testImports("env.a", a, "env.b", b)})
			if err != nil {
				c.Close()
				t.Fatal(err)
			}
			assertDistinctLeafRoute(t, in, enabled && !codeProfileEnabled)
			state := uint32(0x9e3779b9)
			for i := 0; i < 128; i++ {
				state = state*1664525 + 1013904223
				x := int32(state)
				state = state*1664525 + 1013904223
				y := int32(state)
				got, err := in.Invoke("run", uint64(uint32(x))|0xdeadbeef00000000, uint64(uint32(y))|0xaabbccdd00000000)
				want := I32(b(a(x, y), y))
				if err != nil || len(got) != 1 || got[0] != want {
					t.Fatalf("binary reverse=%t enabled=%t: %x,%v want %x", reverse, enabled, got, err, want)
				}
			}
			in.Close()
			c.Close()
		}
	}
}
