//go:build linux && riscv64 && !wago_precompiled && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"errors"
	"math"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64GenericTable64MixedSource() []byte {
	types := [][]byte{
		wasmtest.FuncType([]wasm.ValType{wasm.F64}, []wasm.ValType{wasm.F64}),
		wasmtest.FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128}),
		wasmtest.FuncType([]wasm.ValType{wasm.F64, wasm.I64}, []wasm.ValType{wasm.F64}),
		wasmtest.FuncType([]wasm.ValType{wasm.V128, wasm.I64}, []wasm.ValType{wasm.V128}),
		wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I32, wasm.I32}, nil),
		wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I64}, nil),
		wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}),
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I64}),
		wasmtest.FuncType(nil, nil),
		wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType([]wasm.ValType{wasm.I64}, nil),
		wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I64, wasm.I64}, nil),
	}
	functions := []byte{13, 0, 1, 2, 3, 4, 4, 5, 6, 7, 8, 9, 10, 11}
	names := []string{"float", "vector", "copy", "init", "fill", "grow", "size", "drop", "is_null", "set_null", "copy64"}
	exports := make([][]byte, 0, len(names)+1)
	for index, name := range names {
		exports = append(exports, wasmtest.ExportEntry(name, 0, uint32(index+2)))
	}
	exports = append(exports, wasmtest.ExportEntry("table", 1, 2))
	body := [][]byte{
		{0x20, 0, 0x0b}, {0x20, 0, 0x0b},
		{0x20, 0, 0x20, 1, 0x11, 0, 2, 0x0b},
		{0x20, 0, 0x20, 1, 0x11, 1, 2, 0x0b},
		{0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 14, 2, 1, 0x0b},
		{0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 12, 2, 2, 0x0b},
		{0x20, 0, 0xd0, 0x70, 0x20, 1, 0xfc, 17, 2, 0x0b},
		{0xd0, 0x70, 0x20, 0, 0xfc, 15, 2, 0x0b},
		{0xfc, 16, 2, 0x0b}, {0xfc, 13, 2, 0x0b},
		{0x20, 0, 0x25, 2, 0xd1, 0x0b},
		{0x20, 0, 0xd0, 0x70, 0x26, 2, 0x0b},
		{0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 14, 2, 2, 0x0b},
	}
	code := make([][]byte, len(body))
	for i := range body {
		code[i] = wasmtest.Code(body[i])
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, functions),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 5, 4, 8}, []byte{0x70, 1, 4, 8}, []byte{0x70, 5, 4, 8})),
		wasmtest.Section(7, wasmtest.Vec(exports...)),
		wasmtest.Section(9, wasmtest.Vec([]byte{2, 0, 0x42, 0, 0x0b, 0, 2, 0, 1}, []byte{2, 1, 0x41, 0, 0x0b, 0, 2, 0, 1}, []byte{1, 0, 2, 0, 1})),
		wasmtest.Section(10, wasmtest.Vec(code...)))
}

func rv64GenericTable64CapacitySource(min, max uint64) []byte {
	appendLimit := func(dst []byte, value uint64) []byte {
		for value >= 128 {
			dst = append(dst, byte(value)|0x80)
			value >>= 7
		}
		return append(dst, byte(value))
	}
	table := appendLimit([]byte{0x70, 5}, min)
	table = appendLimit(table, max)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(4, wasmtest.Vec(table)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("size", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xfc, 16, 0, 0x0b}))))
}

func TestRV64GenericTable64CapacityUsesQuota(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, limits := range [][3]uint64{{2048, 2048, 1024}, {2048, 2048, 1 << 20}, {0, ^uint64(0), 1 << 20}, {2048, ^uint64(0), 1 << 20}} {
			cfg := rv64NativeTailConfig().WithMaxInstanceMetadataBytes(limits[2])
			c, err := cfg.Compile(rv64GenericTable64CapacitySource(limits[0], limits[1]))
			if err != nil {
				t.Fatalf("artifact=%v limits=%v: %v", artifact, limits, err)
			}
			defer c.Close()
			if artifact {
				data, err := c.MarshalBinary()
				if err != nil {
					t.Fatal(err)
				}
				c, err = LoadTrustedArtifact(data)
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
			}
			rt := NewRuntime(WithRuntimeConfig(cfg))
			defer rt.Close()
			mod, err := rt.Module(c)
			if err != nil {
				t.Fatal(err)
			}
			defer mod.Close()
			in, err := rt.Instantiate(context.Background(), mod)
			if limits[2] == 1024 {
				var quota *coreruntime.ResourceLimitError
				if !errors.As(err, &quota) {
					t.Fatal("table64 allocation bypassed quota", err)
				}
				if in != nil {
					in.Close()
				}
				continue
			}
			if err != nil {
				t.Fatal(err)
			}
			got, err := in.Invoke("size")
			in.Close()
			if err != nil || !slices.Equal(got, []uint64{limits[0]}) {
				t.Fatal(got, err)
			}
		}
	}
}

func TestRV64GenericTable64RejectsChangedBinding(t *testing.T) {
	c, err := rv64NativeTailConfig().Compile(rv64GenericTable64MixedSource())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	view := c.executionView()
	if got := testing.AllocsPerRun(100, func() {
		if !rv64GenericTable64(view) {
			panic("qualification lost")
		}
	}); got != 0 {
		t.Fatal("warm qualification allocations", got)
	}
	for _, mutate := range []func(*Compiled){
		func(bad *Compiled) { bad.requiredFeatures &^= CoreFeatureTable64 },
		func(bad *Compiled) { bad.code = append([]byte(nil), bad.code...); bad.code[0] ^= 1 },
		func(bad *Compiled) { bad.TableSize++ },
		func(bad *Compiled) { bad.TableAddr64 = !bad.TableAddr64 },
		func(bad *Compiled) { bad.extraTables[0].HasValueType = false },
	} {
		bad := cloneCompiledMetadata(view)
		mutate(bad)
		if rv64GenericTable64(bad) || railshotValidateCompiled(bad) == nil {
			t.Fatal("changed Table64 binding admitted")
		}
	}
}

func TestRV64GenericTable64ReservationBindingAndBudget(t *testing.T) {
	c, err := rv64NativeTailConfig().Compile(rv64GenericTable64CapacitySource(0, ^uint64(0)))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	view := c.executionView()
	if !slices.Equal(view.rv64ExecutionTableCapacities(), []uint32{0}) || view.TableMax != ^uint64(0) {
		t.Fatal("reservation erased declared maximum")
	}
	if got := railshotTableCapacitySnapshotBytes(view); got < 28 {
		t.Fatal("reservation cold state absent from snapshot budget", got)
	}
	bad := cloneCompiledMetadata(view)
	indexes := new(compiledCacheIndexes)
	indexes.railshotTail = view.loadCompileIndexes().railshotTail
	bad.validateMemo = &validateMemo{compileIndexes: indexes}
	bad.setRV64ExecutionTableCapacities([]uint32{1})
	if rv64GenericTable64(bad) || bad.validate() == nil {
		t.Fatal("changed reservation kept its authenticated binding")
	}
	if !slices.Equal(view.rv64ExecutionTableCapacities(), []uint32{0}) {
		t.Fatal("mutable candidate changed the execution reservation")
	}
}

func BenchmarkRV64GenericTable64WarmSize(b *testing.B) {
	c, err := rv64NativeTailConfig().Compile(rv64GenericTable64MixedSource())
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c)
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	if got, err := in.Invoke("size"); err != nil || !slices.Equal(got, []uint64{4}) {
		b.Fatal(got, err)
	}
	b.ReportMetric(float64(len(c.code)), "code-B")
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		got, err := in.Invoke("size")
		if err != nil || !slices.Equal(got, []uint64{4}) {
			b.Fatal(got, err)
		}
	}
}

func TestRV64GenericTable64MixedOperationsFloatSIMDSourceArtifact(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		cfg := rv64NativeTailConfig()
		cfg = cfg.WithCoreFeatures(cfg.features | CoreFeatureTable64 | CoreFeatureSIMD | CoreFeatureBulkMemoryOperations | CoreFeatureReferenceTypes)
		code, err := cfg.Compile(rv64GenericTable64MixedSource())
		if err != nil {
			t.Fatal(err)
		}
		defer code.Close()
		if code.nativeTable64ABI != nativeTable64NoABI || !rv64GenericTable64(code.executionView()) {
			t.Fatal("general module borrowed an exact table64 witness")
		}
		if artifact {
			blob, err := code.MarshalBinary()
			if err != nil {
				t.Fatal(err)
			}
			code, err = LoadTrustedArtifact(blob)
			if err != nil {
				t.Fatal(err)
			}
			defer code.Close()
		}
		in, err := Instantiate(code)
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		call := func(name string, want []uint64, args ...uint64) {
			t.Helper()
			got, err := in.Invoke(name, args...)
			if err != nil || !slices.Equal(got, want) {
				t.Fatalf("artifact=%v %s: %v, %v; want %v", artifact, name, got, err, want)
			}
		}
		trap := func(name string, args ...uint64) {
			t.Helper()
			if _, err := in.Invoke(name, args...); err == nil {
				t.Fatalf("artifact=%v %s did not reject invalid range", artifact, name)
			}
		}
		float := math.Float64bits(-17.25)
		vector := []uint64{0xfedcba9876543210, 0x0123456789abcdef}
		call("copy", nil, 0, 0, 2)
		call("float", []uint64{float}, float, 0)
		call("vector", vector, vector[0], vector[1], 1)
		trap("float", float, ^uint64(0))
		trap("float", float, 1)
		trap("copy", ^uint64(0), 0, 0)
		call("copy64", nil, 1, 0, 2)
		call("float", []uint64{float}, float, 1)
		call("vector", vector, vector[0], vector[1], 2)
		call("fill", nil, 0, 3)
		call("is_null", []uint64{1}, 2)
		call("init", nil, 0, 0, 2)
		call("float", []uint64{float}, float, 0)
		call("grow", []uint64{4}, 2)
		call("size", []uint64{6})
		call("grow", []uint64{^uint64(0)}, ^uint64(0))
		call("set_null", nil, 0)
		call("is_null", []uint64{1}, 0)
		call("drop", nil)
		trap("init", 0, 0, 1)
		trap("fill", 0, ^uint64(0))
		call("size", []uint64{6})
	}
}

func TestRV64GenericTypedTable64SourceArtifact(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, []byte{2, 0, 1}),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x63, 0, 5, 2, 4}, []byte{0x70, 1, 2, 4})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 1))),
		wasmtest.Section(9, wasmtest.Vec([]byte{6, 0, 0x42, 0, 0x0b, 0x63, 0, 1, 0xd2, 0, 0x0b})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 42, 0x0b}), wasmtest.Code([]byte{0x20, 0, 0x11, 0, 0, 0x0b}))))
	for _, artifact := range []bool{false, true} {
		code, err := rv64NativeTailConfig().Compile(source)
		if err != nil {
			t.Fatal(err)
		}
		defer code.Close()
		if artifact {
			blob, err := code.MarshalBinary()
			if err != nil {
				t.Fatal(err)
			}
			code, err = LoadTrustedArtifact(blob)
			if err != nil {
				t.Fatal(err)
			}
			defer code.Close()
		}
		in, err := Instantiate(code)
		if err != nil {
			t.Fatal(err)
		}
		for _, index := range []uint64{0, 1, ^uint64(0), 0} {
			got, err := in.Invoke("call", index)
			if index == 0 {
				if err != nil || !slices.Equal(got, []uint64{42}) {
					t.Fatalf("artifact=%v call: %v %v", artifact, got, err)
				}
			} else if err == nil {
				t.Fatal("invalid typed table64 target did not trap", index)
			}
		}
		in.Close()
	}
}

func TestRV64GenericTable64EmptyLargeMaximumReservation(t *testing.T) {
	table := []byte{0x70, 5, 0x80, 0x10, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 1}
	c, err := rv64NativeTailConfig().Compile(wasmtest.Module(wasmtest.Section(4, wasmtest.Vec(table))))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	blob, err := c.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadTrustedArtifact(blob)
	if err != nil {
		t.Fatal(err)
	}
	defer loaded.Close()
	for _, code := range []*Compiled{c, loaded} {
		if code.TableMax != ^uint64(0) || code.tableRuntimeCapacity(0) != 2048 || len(code.rv64ExecutionTableCapacities()) != 0 {
			t.Fatal("inert table maximum/reservation or redundant directory", code.TableMax, code.tableRuntimeCapacity(0), code.rv64ExecutionTableCapacities())
		}
		in, err := Instantiate(code)
		if err != nil {
			t.Fatal(err)
		}
		in.Close()
	}
}
