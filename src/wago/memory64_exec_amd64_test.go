//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"bytes"
	"encoding/binary"
	"reflect"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func uleb64(v uint64) []byte {
	var out []byte
	for {
		b := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			b |= 0x80
		}
		out = append(out, b)
		if v == 0 {
			return out
		}
	}
}

func memory64LimitsModule(max *uint64) []byte {
	flags := byte(0x04)
	if max != nil {
		flags = 0x05
	}
	mem := append([]byte{flags}, uleb64(1)...)
	if max != nil {
		mem = append(mem, uleb64(*max)...)
	}
	memarg := func(op byte, offset uint64) []byte {
		out := []byte{op, 0x02}
		return append(out, uleb64(offset)...)
	}
	storeLoad := []byte{0x20, 0x00, 0x20, 0x01}
	storeLoad = append(storeLoad, memarg(0x36, 0)...)
	storeLoad = append(storeLoad, 0x20, 0x00)
	storeLoad = append(storeLoad, memarg(0x28, 0)...)
	storeLoad = append(storeLoad, 0x0b)
	offsetLoad := []byte{0x20, 0x00}
	offsetLoad = append(offsetLoad, memarg(0x28, ^uint64(0))...)
	offsetLoad = append(offsetLoad, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I64}),
			wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}),
			wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(3))),
		wasmtest.Section(5, wasmtest.Vec(mem)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("size", 0, 0),
			wasmtest.ExportEntry("grow", 0, 1),
			wasmtest.ExportEntry("store_load", 0, 2),
			wasmtest.ExportEntry("offset_load", 0, 3),
			wasmtest.ExportEntry("memory", 2, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x3f, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x40, 0x00, 0x0b}),
			wasmtest.Code(storeLoad),
			wasmtest.Code(offsetLoad),
		)),
	)
}

func boundedMemory64Module(max uint64) []byte {
	return memory64LimitsModule(&max)
}

func unboundedMemory64Module() []byte {
	return memory64LimitsModule(nil)
}

type memory64ScalarOp struct {
	name      string
	opcode    byte
	align     byte
	width     int
	resultI64 bool
	store     bool
}

var memory64IntegerScalarOps = []memory64ScalarOp{
	{name: "i32.load", opcode: 0x28, align: 2, width: 4},
	{name: "i64.load", opcode: 0x29, align: 3, width: 8, resultI64: true},
	{name: "i32.load8_s", opcode: 0x2c, width: 1},
	{name: "i32.load8_u", opcode: 0x2d, width: 1},
	{name: "i32.load16_s", opcode: 0x2e, align: 1, width: 2},
	{name: "i32.load16_u", opcode: 0x2f, align: 1, width: 2},
	{name: "i64.load8_s", opcode: 0x30, width: 1, resultI64: true},
	{name: "i64.load8_u", opcode: 0x31, width: 1, resultI64: true},
	{name: "i64.load16_s", opcode: 0x32, align: 1, width: 2, resultI64: true},
	{name: "i64.load16_u", opcode: 0x33, align: 1, width: 2, resultI64: true},
	{name: "i64.load32_s", opcode: 0x34, align: 2, width: 4, resultI64: true},
	{name: "i64.load32_u", opcode: 0x35, align: 2, width: 4, resultI64: true},
	{name: "i32.store", opcode: 0x36, align: 2, width: 4, store: true},
	{name: "i64.store", opcode: 0x37, align: 3, width: 8, resultI64: true, store: true},
	{name: "i32.store8", opcode: 0x3a, width: 1, store: true},
	{name: "i32.store16", opcode: 0x3b, align: 1, width: 2, store: true},
	{name: "i64.store8", opcode: 0x3c, width: 1, resultI64: true, store: true},
	{name: "i64.store16", opcode: 0x3d, align: 1, width: 2, resultI64: true, store: true},
	{name: "i64.store32", opcode: 0x3e, align: 2, width: 4, resultI64: true, store: true},
}

func memory64IntegerScalarModule() []byte {
	types := wasmtest.Vec(
		wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}),
		wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I32}, nil),
		wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I64}, nil),
	)
	funcs := make([][]byte, 0, len(memory64IntegerScalarOps))
	exports := make([][]byte, 0, len(memory64IntegerScalarOps)+1)
	codes := make([][]byte, 0, len(memory64IntegerScalarOps))
	for i, op := range memory64IntegerScalarOps {
		typeIndex := uint32(0)
		if op.resultI64 {
			typeIndex = 1
		}
		body := []byte{0x20, 0x00}
		if op.store {
			typeIndex += 2
			body = append(body, 0x20, 0x01)
		}
		body = append(body, op.opcode, op.align, 0x00, 0x0b)
		funcs = append(funcs, wasmtest.ULEB(typeIndex))
		exports = append(exports, wasmtest.ExportEntry(op.name, 0, uint32(i)))
		codes = append(codes, wasmtest.Code(body))
	}
	memory := append([]byte{0x05}, uleb64(1)...)
	memory = append(memory, uleb64(2)...)
	exports = append(exports, wasmtest.ExportEntry("memory", 2, 0))
	return wasmtest.Module(
		wasmtest.Section(1, types),
		wasmtest.Section(3, wasmtest.Vec(funcs...)),
		wasmtest.Section(5, wasmtest.Vec(memory)),
		wasmtest.Section(7, wasmtest.Vec(exports...)),
		wasmtest.Section(10, wasmtest.Vec(codes...)),
	)
}

func memory64FloatScalarModule() []byte {
	memory := append([]byte{0x05}, uleb64(1)...)
	memory = append(memory, uleb64(2)...)
	memop := func(op, align byte, offset uint64) []byte {
		out := []byte{op, align}
		return append(out, uleb64(offset)...)
	}
	f32Load := append([]byte{0x20, 0x00}, memop(0x2a, 2, 0)...)
	f32Load = append(f32Load, 0x0b)
	f64Load := append([]byte{0x20, 0x00}, memop(0x2b, 3, 0)...)
	f64Load = append(f64Load, 0x0b)
	f32Store := append([]byte{0x20, 0x00, 0x20, 0x01}, memop(0x38, 2, 0)...)
	f32Store = append(f32Store, 0x0b)
	f64Store := append([]byte{0x20, 0x00, 0x20, 0x01}, memop(0x39, 3, 0)...)
	f64Store = append(f64Store, 0x0b)
	offsetLoad := append([]byte{0x20, 0x00}, memop(0x2a, 2, ^uint64(0))...)
	offsetLoad = append(offsetLoad, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.F32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.F64}),
			wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.F32}, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.F64}, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(3), wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec(memory)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("f32.load", 0, 0), wasmtest.ExportEntry("f64.load", 0, 1),
			wasmtest.ExportEntry("f32.store", 0, 2), wasmtest.ExportEntry("f64.store", 0, 3),
			wasmtest.ExportEntry("offset.load", 0, 4), wasmtest.ExportEntry("memory", 2, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(f32Load), wasmtest.Code(f64Load), wasmtest.Code(f32Store), wasmtest.Code(f64Store), wasmtest.Code(offsetLoad),
		)),
	)
}

func memory64SIMDMemOp(sub uint32, align byte, offset uint64, lane ...byte) []byte {
	op := []byte{0xfd}
	op = append(op, wasmtest.ULEB(sub)...)
	op = append(op, align)
	op = append(op, uleb64(offset)...)
	return append(op, lane...)
}

func memory64SIMDModule(params, results []wasm.ValType, body []byte) []byte {
	memory := append([]byte{0x05}, uleb64(1)...)
	memory = append(memory, uleb64(2)...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, results))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec(memory)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("run", 0, 0),
			wasmtest.ExportEntry("memory", 2, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func memory64ActiveDataModule(offset int64, payload []byte) []byte {
	memory := append([]byte{0x05}, uleb64(1)...)
	memory = append(memory, uleb64(2)...)
	segment := []byte{0x00, 0x42}
	segment = append(segment, wasmtest.SLEB64(offset)...)
	segment = append(segment, 0x0b)
	segment = append(segment, wasmtest.ULEB(uint32(len(payload)))...)
	segment = append(segment, payload...)
	return wasmtest.Module(
		wasmtest.Section(5, wasmtest.Vec(memory)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("memory", 2, 0))),
		wasmtest.Section(11, wasmtest.Vec(segment)),
	)
}

func memory64PassiveDataModule(payload []byte) []byte {
	memory := append([]byte{0x05}, uleb64(1)...)
	memory = append(memory, uleb64(2)...)
	segment := append([]byte{0x01}, wasmtest.ULEB(uint32(len(payload)))...)
	segment = append(segment, payload...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I32, wasm.I32}, nil),
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(5, wasmtest.Vec(memory)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("init", 0, 0),
			wasmtest.ExportEntry("drop", 0, 1),
			wasmtest.ExportEntry("memory", 2, 0),
		)),
		wasmtest.Section(12, wasmtest.ULEB(1)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0xfc, 0x08, 0x00, 0x00, 0x0b}),
			wasmtest.Code([]byte{0xfc, 0x09, 0x00, 0x0b}),
		)),
		wasmtest.Section(11, wasmtest.Vec(segment)),
	)
}

func compileStagedMemory64(data []byte) (*Compiled, error) {
	cfg := NewRuntimeConfig()
	features := cfg.frontendFeatures()
	features.Memory64 = true
	return compileWithFrontendFeatures(cfg, data, features)
}

func TestStagedMemory64LocalExecutionAndProductRoundTrip(t *testing.T) {
	module := boundedMemory64Module(3)
	if _, err := Compile(nil, module); err == nil || !strings.Contains(err.Error(), "memory64") {
		t.Fatalf("public memory64 compile error = %v, want fail-closed feature rejection", err)
	}
	compiled, err := compileStagedMemory64(module)
	if err != nil {
		t.Fatalf("staged memory64 compile: %v", err)
	}
	defer compiled.Close()
	if compiled.MemMinPages != 1 || compiled.MemMaxPages != 3 || !compiled.HasMemory {
		t.Fatalf("memory64 execution cache = present %v min/max %d/%d", compiled.HasMemory, compiled.MemMinPages, compiled.MemMaxPages)
	}
	meta := (&Module{c: compiled}).Metadata()
	if len(meta.Memories) != 1 || !meta.Memories[0].Addr64 || meta.Memories[0].Min != 1 || meta.Memories[0].Max != 3 || !meta.Memories[0].HasMax || !reflect.DeepEqual(meta.Memories[0].Exports, []string{"memory"}) {
		t.Fatalf("memory64 metadata = %#v", meta.Memories)
	}
	if err := applyPolicy(&Module{c: compiled}, Policy{MaxMemoryBytes: 3 * 65536}); err != nil {
		t.Fatalf("exact memory64 reservation policy: %v", err)
	}
	if err := applyPolicy(&Module{c: compiled}, Policy{MaxMemoryBytes: 2 * 65536}); err == nil {
		t.Fatal("memory64 reservation above policy limit was accepted")
	}
	blob, err := compiled.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal staged memory64: %v", err)
	}
	var public Compiled
	if err := public.UnmarshalBinary(blob); err == nil || !strings.Contains(err.Error(), "unknown required feature bits") {
		t.Fatalf("public memory64 codec load error = %v", err)
	}
	t.Logf("staged memory64 product: wasm=%d code=%d codec=%d reservation=%d bytes", len(module), len(compiled.Code), len(blob), 3*65536)
	var loaded Compiled
	if err := unmarshalCompiled(&loaded, blob[5:]); err != nil {
		t.Fatalf("decode staged memory64 metadata: %v", err)
	}
	defer loaded.Close()
	loaded.memoryDir.exactExports = true
	if !reflect.DeepEqual((&Module{c: &loaded}).Metadata().Memories, meta.Memories) {
		t.Fatalf("memory64 codec metadata changed: got %#v want %#v", (&Module{c: &loaded}).Metadata().Memories, meta.Memories)
	}

	in, err := instantiateCore(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate staged memory64: %v", err)
	}
	defer in.Close()
	call := func(name string, args ...uint64) []uint64 {
		t.Helper()
		got, err := in.Invoke(name, args...)
		if err != nil {
			t.Fatalf("%s%v: %v", name, args, err)
		}
		return got
	}
	if got := call("size"); len(got) != 1 || got[0] != 1 {
		t.Fatalf("initial memory64.size = %v, want [1]", got)
	}
	if got := call("store_load", 8, I32(0x12345678)); len(got) != 1 || uint32(got[0]) != 0x12345678 {
		t.Fatalf("memory64 store/load = %v", got)
	}
	if got := call("grow", 1); len(got) != 1 || got[0] != 1 {
		t.Fatalf("memory64.grow(1) = %v, want [1]", got)
	}
	if got := call("size"); len(got) != 1 || got[0] != 2 {
		t.Fatalf("grown memory64.size = %v, want [2]", got)
	}
	if got := call("store_load", 65536, uint64(0xabcdef01)); len(got) != 1 || uint32(got[0]) != 0xabcdef01 {
		t.Fatalf("grown-page memory64 store/load = %v", got)
	}
	if got := call("grow", 1<<32); len(got) != 1 || got[0] != ^uint64(0) {
		t.Fatalf("memory64.grow(2^32) = %v, want [-1]", got)
	}
	if got := call("size"); len(got) != 1 || got[0] != 2 {
		t.Fatalf("failed grow changed memory64.size = %v", got)
	}
	if _, err := in.Invoke("store_load", ^uint64(0), I32(1)); err == nil || !strings.Contains(err.Error(), "out of bounds") {
		t.Fatalf("memory64 address overflow error = %v", err)
	}
	if _, err := in.Invoke("offset_load", 1); err == nil || !strings.Contains(err.Error(), "out of bounds") {
		t.Fatalf("memory64 offset overflow error = %v", err)
	}
}

func TestStagedMemory64NoMaximumUsesFiniteReservation(t *testing.T) {
	module := unboundedMemory64Module()
	compiled, err := compileStagedMemory64(module)
	if err != nil {
		t.Fatalf("compile no-max memory64: %v", err)
	}
	defer compiled.Close()
	if compiled.MemMinPages != 1 || compiled.MemMaxPages != 65535 {
		t.Fatalf("no-max memory64 execution reservation = %d/%d, want 1/65535 pages", compiled.MemMinPages, compiled.MemMaxPages)
	}
	meta := (&Module{c: compiled}).Metadata()
	if len(meta.Memories) != 1 || !meta.Memories[0].Addr64 || meta.Memories[0].HasMax || meta.Memories[0].Max != 0 {
		t.Fatalf("no-max memory64 metadata = %#v", meta.Memories)
	}
	const reserveBytes = uint64(65535) * 65536
	if err := applyPolicy(&Module{c: compiled}, Policy{MaxMemoryBytes: reserveBytes}); err != nil {
		t.Fatalf("finite no-max memory64 reservation policy: %v", err)
	}
	if err := applyPolicy(&Module{c: compiled}, Policy{MaxMemoryBytes: reserveBytes - 1}); err == nil {
		t.Fatal("no-max memory64 reservation below policy limit was accepted")
	}
	blob, err := compiled.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal no-max memory64: %v", err)
	}
	var loaded Compiled
	if err := unmarshalCompiled(&loaded, blob[5:]); err != nil {
		t.Fatalf("unmarshal no-max memory64: %v", err)
	}
	defer loaded.Close()
	loaded.memoryDir.stagedMemory64 = true
	loaded.memoryDir.exactExports = true
	loadedMeta := (&Module{c: &loaded}).Metadata()
	if !reflect.DeepEqual(loadedMeta.Memories, meta.Memories) || loaded.MemMaxPages != 65535 {
		t.Fatalf("no-max memory64 codec metadata/reservation = %#v/%d, want %#v/65535", loadedMeta.Memories, loaded.MemMaxPages, meta.Memories)
	}

	for name, c := range map[string]*Compiled{"compiled": compiled, "codec": &loaded} {
		t.Run(name, func(t *testing.T) {
			in, err := instantiateCore(c, InstantiateOptions{})
			if err != nil {
				t.Fatalf("instantiate no-max memory64: %v", err)
			}
			defer in.Close()
			if got, err := in.Invoke("grow", 1); err != nil || len(got) != 1 || got[0] != 1 {
				t.Fatalf("no-max memory64.grow(1) = %v, err=%v", got, err)
			}
			if got, err := in.Invoke("grow", 65534); err != nil || len(got) != 1 || got[0] != ^uint64(0) {
				t.Fatalf("no-max memory64 growth past implementation reserve = %v, err=%v", got, err)
			}
			if got, err := in.Invoke("size"); err != nil || len(got) != 1 || got[0] != 2 {
				t.Fatalf("failed no-max memory64 grow changed size = %v, err=%v", got, err)
			}
		})
	}
}

func TestStagedMemory64ActiveDataLifecycle(t *testing.T) {
	module := memory64ActiveDataModule(65532, []byte{1, 2, 3, 4})
	compiled, err := compileStagedMemory64(module)
	if err != nil {
		t.Fatalf("compile memory64 active data: %v", err)
	}
	defer compiled.Close()
	if len(compiled.Data) != 1 || len(compiled.Data[0].Offset.Expr) == 0 || compiled.Data[0].Offset.Base != 0 || compiled.Data[0].Offset.HasGlobal {
		t.Fatalf("memory64 data offset metadata = %#v, want exact i64 expression", compiled.Data)
	}
	blob, err := compiled.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal memory64 data metadata: %v", err)
	}
	var loaded Compiled
	if err := unmarshalCompiled(&loaded, blob[5:]); err != nil {
		t.Fatalf("unmarshal memory64 data metadata: %v", err)
	}
	defer loaded.Close()
	if !reflect.DeepEqual(loaded.Data, compiled.Data) {
		t.Fatalf("memory64 data metadata changed across codec: got %#v want %#v", loaded.Data, compiled.Data)
	}
	if _, err := Capture(compiled, SnapshotOptions{}); err == nil || !strings.Contains(err.Error(), "memory64 modules cannot be snapshotted") {
		t.Fatalf("memory64 snapshot error = %v, want fail-closed lifecycle rejection", err)
	}
	in, err := instantiateCore(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate memory64 active data: %v", err)
	}
	defer in.Close()
	memory, err := in.ExportedMemory("memory")
	if err != nil {
		t.Fatal(err)
	}
	if got := memory.Bytes()[65532:65536]; !bytes.Equal(got, []byte{1, 2, 3, 4}) {
		t.Fatalf("memory64 active data bytes = %v, want [1 2 3 4]", got)
	}

	overflow, err := compileStagedMemory64(memory64ActiveDataModule(-1, []byte{1, 2}))
	if err != nil {
		t.Fatalf("compile overflowing memory64 data offset: %v", err)
	}
	defer overflow.Close()
	if _, err := instantiateCore(overflow, InstantiateOptions{}); err == nil || !strings.Contains(err.Error(), "overflows u64") {
		t.Fatalf("memory64 data offset+length overflow error = %v", err)
	}

}

func TestStagedMemory64PassiveDataLifecycle(t *testing.T) {
	module := memory64PassiveDataModule([]byte("hello"))
	compiled, err := compileStagedMemory64(module)
	if err != nil {
		t.Fatalf("compile memory64 passive data: %v", err)
	}
	defer compiled.Close()
	if len(compiled.PassiveData) != 1 || string(compiled.PassiveData[0].Bytes) != "hello" {
		t.Fatalf("memory64 passive metadata = %#v", compiled.PassiveData)
	}
	blob, err := compiled.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal memory64 passive data: %v", err)
	}
	var loaded Compiled
	if err := unmarshalCompiled(&loaded, blob[5:]); err != nil {
		t.Fatalf("unmarshal memory64 passive data: %v", err)
	}
	defer loaded.Close()
	loaded.memoryDir.stagedMemory64 = true // private execution proof; codec never serializes admission
	if !reflect.DeepEqual(loaded.PassiveData, compiled.PassiveData) {
		t.Fatalf("memory64 passive metadata changed across codec: got %#v want %#v", loaded.PassiveData, compiled.PassiveData)
	}
	if _, err := Capture(compiled, SnapshotOptions{}); err == nil || !strings.Contains(err.Error(), "memory64 modules cannot be snapshotted") {
		t.Fatalf("memory64 passive snapshot error = %v", err)
	}

	for name, c := range map[string]*Compiled{"compiled": compiled, "codec": &loaded} {
		t.Run(name, func(t *testing.T) {
			in, err := instantiateCore(c, InstantiateOptions{})
			if err != nil {
				t.Fatalf("instantiate memory64 passive data: %v", err)
			}
			defer in.Close()
			memory, err := in.ExportedMemory("memory")
			if err != nil {
				t.Fatal(err)
			}
			mem := memory.Bytes()
			if _, err := in.Invoke("init", 16, I32(1), I32(3)); err != nil {
				t.Fatalf("memory64.init: %v", err)
			}
			if got := string(mem[16:19]); got != "ell" {
				t.Fatalf("memory64.init bytes = %q, want ell", got)
			}
			for _, tc := range []struct {
				name string
				dst  uint64
				src  uint64
				n    uint64
			}{
				{name: "destination carry", dst: ^uint64(0), src: 0, n: 2},
				{name: "destination end", dst: uint64(len(mem)), src: 0, n: 1},
				{name: "source end", dst: 32, src: 4, n: 2},
			} {
				t.Run(tc.name, func(t *testing.T) {
					before := append([]byte(nil), mem[16:40]...)
					if _, err := in.Invoke("init", tc.dst, tc.src, tc.n); err == nil || !strings.Contains(err.Error(), "out of bounds") {
						t.Fatalf("memory64.init trap = %v", err)
					}
					if !bytes.Equal(mem[16:40], before) {
						t.Fatal("trapping memory64.init changed memory")
					}
				})
			}
			if _, err := in.Invoke("drop"); err != nil {
				t.Fatalf("memory64 data.drop: %v", err)
			}
			before := append([]byte(nil), mem[16:40]...)
			if _, err := in.Invoke("init", 24, I32(0), I32(1)); err == nil || !strings.Contains(err.Error(), "out of bounds") {
				t.Fatalf("memory64.init after drop trap = %v", err)
			}
			if !bytes.Equal(mem[16:40], before) {
				t.Fatal("memory64.init after drop changed memory")
			}
			if _, err := in.Invoke("init", uint64(len(mem)), I32(0), I32(0)); err != nil {
				t.Fatalf("zero-length memory64.init after drop: %v", err)
			}
		})
	}
}

func TestStagedMemory64IntegerScalarFamily(t *testing.T) {
	module := memory64IntegerScalarModule()
	compiled, err := compileStagedMemory64(module)
	if err != nil {
		t.Fatalf("compile memory64 integer scalar family: %v", err)
	}
	defer compiled.Close()
	t.Logf("memory64 integer scalar family: wasm=%d code=%d operations=%d", len(module), len(compiled.Code), len(memory64IntegerScalarOps))
	in, err := instantiateCore(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate memory64 integer scalar family: %v", err)
	}
	defer in.Close()
	memory, err := in.ExportedMemory("memory")
	if err != nil {
		t.Fatal(err)
	}
	mem := memory.Bytes()
	const addr = 64
	loadCases := map[string]struct {
		bits uint64
		want uint64
	}{
		"i32.load":     {bits: 0x89abcdef, want: 0x89abcdef},
		"i64.load":     {bits: 0x0123456789abcdef, want: 0x0123456789abcdef},
		"i32.load8_s":  {bits: 0x80, want: uint64(uint32(0xffffff80))},
		"i32.load8_u":  {bits: 0x80, want: 0x80},
		"i32.load16_s": {bits: 0x8000, want: uint64(uint32(0xffff8000))},
		"i32.load16_u": {bits: 0x8000, want: 0x8000},
		"i64.load8_s":  {bits: 0x80, want: 0xffffffffffffff80},
		"i64.load8_u":  {bits: 0x80, want: 0x80},
		"i64.load16_s": {bits: 0x8000, want: 0xffffffffffff8000},
		"i64.load16_u": {bits: 0x8000, want: 0x8000},
		"i64.load32_s": {bits: 0x80000000, want: 0xffffffff80000000},
		"i64.load32_u": {bits: 0x80000000, want: 0x80000000},
	}
	for _, op := range memory64IntegerScalarOps {
		op := op
		t.Run(op.name, func(t *testing.T) {
			if op.store {
				for i := 0; i < 9; i++ {
					mem[addr+i] = 0xa5
				}
				value := uint64(0x1122334455667788)
				if !op.resultI64 {
					value = uint64(uint32(value))
				}
				if _, err := in.Invoke(op.name, addr, value); err != nil {
					t.Fatalf("store: %v", err)
				}
				var encoded [8]byte
				binary.LittleEndian.PutUint64(encoded[:], value)
				if !bytes.Equal(mem[addr:addr+op.width], encoded[:op.width]) || mem[addr+op.width] != 0xa5 {
					t.Fatalf("stored bytes = %x sentinel=%x, want %x/a5", mem[addr:addr+op.width], mem[addr+op.width], encoded[:op.width])
				}
				if _, err := in.Invoke(op.name, uint64(len(mem)-op.width+1), value); err == nil || !strings.Contains(err.Error(), "out of bounds") {
					t.Fatalf("end-of-memory store error = %v", err)
				}
				return
			}
			tc := loadCases[op.name]
			var encoded [8]byte
			binary.LittleEndian.PutUint64(encoded[:], tc.bits)
			copy(mem[addr:addr+op.width], encoded[:op.width])
			got, err := in.Invoke(op.name, addr)
			if err != nil || len(got) != 1 || got[0] != tc.want {
				t.Fatalf("load = %v, err=%v, want [%#x]", got, err, tc.want)
			}
			if _, err := in.Invoke(op.name, uint64(len(mem)-op.width+1)); err == nil || !strings.Contains(err.Error(), "out of bounds") {
				t.Fatalf("end-of-memory load error = %v", err)
			}
		})
	}
}

func TestStagedMemory64FloatScalarFamily(t *testing.T) {
	compiled, err := compileStagedMemory64(memory64FloatScalarModule())
	if err != nil {
		t.Fatalf("compile memory64 float scalar family: %v", err)
	}
	defer compiled.Close()
	in, err := instantiateCore(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate memory64 float scalar family: %v", err)
	}
	defer in.Close()
	memory, err := in.ExportedMemory("memory")
	if err != nil {
		t.Fatal(err)
	}
	mem := memory.Bytes()
	const addr = 96
	f32bits := uint32(0x7fa00001)
	f64bits := uint64(0x7ff4000000000001)
	binary.LittleEndian.PutUint32(mem[addr:], f32bits)
	if got, err := in.Invoke("f32.load", addr); err != nil || len(got) != 1 || uint32(got[0]) != f32bits {
		t.Fatalf("f32.load bits = %v, err=%v, want %#x", got, err, f32bits)
	}
	binary.LittleEndian.PutUint64(mem[addr:], f64bits)
	if got, err := in.Invoke("f64.load", addr); err != nil || len(got) != 1 || got[0] != f64bits {
		t.Fatalf("f64.load bits = %v, err=%v, want %#x", got, err, f64bits)
	}
	for i := 0; i < 9; i++ {
		mem[addr+i] = 0xa5
	}
	if _, err := in.Invoke("f32.store", addr, uint64(f32bits)); err != nil {
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint32(mem[addr:]); got != f32bits || mem[addr+4] != 0xa5 {
		t.Fatalf("f32.store bytes = %#x sentinel=%#x", got, mem[addr+4])
	}
	if _, err := in.Invoke("f64.store", addr, f64bits); err != nil {
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint64(mem[addr:]); got != f64bits || mem[addr+8] != 0xa5 {
		t.Fatalf("f64.store bytes = %#x sentinel=%#x", got, mem[addr+8])
	}
	for _, tc := range []struct {
		name  string
		addr  uint64
		value []uint64
		width int
	}{{"f32.load", uint64(len(mem) - 3), nil, 4}, {"f64.load", uint64(len(mem) - 7), nil, 8}, {"f32.store", uint64(len(mem) - 3), []uint64{uint64(f32bits)}, 4}, {"f64.store", uint64(len(mem) - 7), []uint64{f64bits}, 8}} {
		args := append([]uint64{tc.addr}, tc.value...)
		if _, err := in.Invoke(tc.name, args...); err == nil || !strings.Contains(err.Error(), "out of bounds") {
			t.Fatalf("%s width %d end error = %v", tc.name, tc.width, err)
		}
	}
	if _, err := in.Invoke("offset.load", 1); err == nil || !strings.Contains(err.Error(), "out of bounds") {
		t.Fatalf("float offset overflow error = %v", err)
	}
	t.Logf("memory64 float scalar family: wasm=%d code=%d operations=4", len(memory64FloatScalarModule()), len(compiled.Code))
}

func TestStagedMemory64SIMDMemoryFamily(t *testing.T) {
	if !hostSupportsSIMD() {
		t.Skip("host SIMD unavailable")
	}
	compile := func(t *testing.T, params, results []wasm.ValType, body []byte) (*Compiled, *Instance, *Memory) {
		t.Helper()
		compiled, err := compileStagedMemory64(memory64SIMDModule(params, results, body))
		if err != nil {
			t.Fatalf("compile memory64 SIMD: %v", err)
		}
		t.Cleanup(func() { _ = compiled.Close() })
		in, err := instantiateCore(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatalf("instantiate memory64 SIMD: %v", err)
		}
		t.Cleanup(func() { _ = in.Close() })
		memory, err := in.ExportedMemory("memory")
		if err != nil {
			t.Fatal(err)
		}
		return compiled, in, memory
	}

	t.Run("v128 load store and u64 carry", func(t *testing.T) {
		body := append([]byte{0x20, 0x00, 0x20, 0x01}, memory64SIMDMemOp(11, 4, 5)...)
		body = append(body, 0x20, 0x00)
		body = append(body, memory64SIMDMemOp(0, 4, 5)...)
		body = append(body, 0x0b)
		_, in, memory := compile(t, []wasm.ValType{wasm.I64, wasm.V128}, []wasm.ValType{wasm.V128}, body)
		want := V128{0xde, 0xad, 0xbe, 0xef, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}
		lo, hi := v128RawSlots(want)
		if got := invokeV128(t, in, "run", 3, lo, hi); got != want {
			t.Fatalf("memory64 v128 round trip = % x, want % x", got, want)
		}
		if got := V128(memory.Bytes()[8:24]); got != want {
			t.Fatalf("memory64 v128 bytes = % x, want % x", got, want)
		}
		if _, err := in.Invoke("run", uint64(len(memory.Bytes())-20), lo, hi); err == nil || !strings.Contains(err.Error(), "out of bounds") {
			t.Fatalf("memory64 v128 end trap = %v", err)
		}

		overflowBody := append([]byte{0x20, 0x00}, memory64SIMDMemOp(0, 4, ^uint64(0))...)
		overflowBody = append(overflowBody, 0x0b)
		_, overflow, _ := compile(t, []wasm.ValType{wasm.I64}, []wasm.ValType{wasm.V128}, overflowBody)
		if _, err := overflow.Invoke("run", 1); err == nil || !strings.Contains(err.Error(), "out of bounds") {
			t.Fatalf("memory64 SIMD offset carry error = %v", err)
		}
	})

	loads := []struct {
		name        string
		sub         uint32
		align       byte
		size        int
		input, want V128
	}{
		{name: "v128.load8x8_s", sub: 1, align: 3, size: 8, input: V128{0, 1, 0x7f, 0x80, 0xff, 0xa5, 0x34, 0xfe}, want: V128{0, 0, 1, 0, 0x7f, 0, 0x80, 0xff, 0xff, 0xff, 0xa5, 0xff, 0x34, 0, 0xfe, 0xff}},
		{name: "v128.load8x8_u", sub: 2, align: 3, size: 8, input: V128{0, 1, 0x7f, 0x80, 0xff, 0xa5, 0x34, 0xfe}, want: V128{0, 0, 1, 0, 0x7f, 0, 0x80, 0, 0xff, 0, 0xa5, 0, 0x34, 0, 0xfe, 0}},
		{name: "v128.load16x4_s", sub: 3, align: 3, size: 8, input: V128{0, 0, 0xff, 0x7f, 0, 0x80, 0x34, 0xff}, want: V128{0, 0, 0, 0, 0xff, 0x7f, 0, 0, 0, 0x80, 0xff, 0xff, 0x34, 0xff, 0xff, 0xff}},
		{name: "v128.load16x4_u", sub: 4, align: 3, size: 8, input: V128{0, 0, 0xff, 0x7f, 0, 0x80, 0x34, 0xff}, want: V128{0, 0, 0, 0, 0xff, 0x7f, 0, 0, 0, 0x80, 0, 0, 0x34, 0xff, 0, 0}},
		{name: "v128.load32x2_s", sub: 5, align: 3, size: 8, input: V128{0xff, 0xff, 0xff, 0x7f, 0, 0, 0, 0x80}, want: V128{0xff, 0xff, 0xff, 0x7f, 0, 0, 0, 0, 0, 0, 0, 0x80, 0xff, 0xff, 0xff, 0xff}},
		{name: "v128.load32x2_u", sub: 6, align: 3, size: 8, input: V128{0xff, 0xff, 0xff, 0x7f, 0, 0, 0, 0x80}, want: V128{0xff, 0xff, 0xff, 0x7f, 0, 0, 0, 0, 0, 0, 0, 0x80, 0, 0, 0, 0}},
		{name: "v128.load8_splat", sub: 7, size: 1, input: V128{0xa5}, want: V128{0xa5, 0xa5, 0xa5, 0xa5, 0xa5, 0xa5, 0xa5, 0xa5, 0xa5, 0xa5, 0xa5, 0xa5, 0xa5, 0xa5, 0xa5, 0xa5}},
		{name: "v128.load16_splat", sub: 8, align: 1, size: 2, input: V128{0x34, 0x12}, want: V128{0x34, 0x12, 0x34, 0x12, 0x34, 0x12, 0x34, 0x12, 0x34, 0x12, 0x34, 0x12, 0x34, 0x12, 0x34, 0x12}},
		{name: "v128.load32_splat", sub: 9, align: 2, size: 4, input: V128{0x78, 0x56, 0x34, 0x12}, want: V128{0x78, 0x56, 0x34, 0x12, 0x78, 0x56, 0x34, 0x12, 0x78, 0x56, 0x34, 0x12, 0x78, 0x56, 0x34, 0x12}},
		{name: "v128.load64_splat", sub: 10, align: 3, size: 8, input: V128{0x88, 0x77, 0x66, 0x55, 0x44, 0x33, 0x22, 0x11}, want: V128{0x88, 0x77, 0x66, 0x55, 0x44, 0x33, 0x22, 0x11, 0x88, 0x77, 0x66, 0x55, 0x44, 0x33, 0x22, 0x11}},
		{name: "v128.load32_zero", sub: 92, align: 2, size: 4, input: V128{0xef, 0xcd, 0xab, 0x89}, want: V128{0xef, 0xcd, 0xab, 0x89}},
		{name: "v128.load64_zero", sub: 93, align: 3, size: 8, input: V128{0x88, 0x77, 0x66, 0x55, 0x44, 0x33, 0x22, 0x11}, want: V128{0x88, 0x77, 0x66, 0x55, 0x44, 0x33, 0x22, 0x11}},
	}
	for _, tc := range loads {
		t.Run(tc.name, func(t *testing.T) {
			body := append([]byte{0x20, 0x00}, memory64SIMDMemOp(tc.sub, tc.align, 5)...)
			body = append(body, 0x0b)
			_, in, memory := compile(t, []wasm.ValType{wasm.I64}, []wasm.ValType{wasm.V128}, body)
			copy(memory.Bytes()[8:], tc.input[:tc.size])
			if got := invokeV128(t, in, "run", 3); got != tc.want {
				t.Fatalf("%s = % x, want % x", tc.name, got, tc.want)
			}
			if _, err := in.Invoke("run", uint64(len(memory.Bytes())-5-tc.size+1)); err == nil || !strings.Contains(err.Error(), "out of bounds") {
				t.Fatalf("%s exact-width end trap = %v", tc.name, err)
			}
		})
	}

	lanes := []struct {
		name              string
		loadSub, storeSub uint32
		align, size, lane byte
	}{{"8", 84, 88, 0, 1, 13}, {"16", 85, 89, 1, 2, 6}, {"32", 86, 90, 2, 4, 2}, {"64", 87, 91, 3, 8, 1}}
	for _, tc := range lanes {
		t.Run("lane"+tc.name, func(t *testing.T) {
			initial := V128{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
			lo, hi := v128RawSlots(initial)
			loadBody := append([]byte{0x20, 0x00, 0x20, 0x01}, memory64SIMDMemOp(tc.loadSub, tc.align, 5, tc.lane)...)
			loadBody = append(loadBody, 0x0b)
			_, loadIn, loadMemory := compile(t, []wasm.ValType{wasm.I64, wasm.V128}, []wasm.ValType{wasm.V128}, loadBody)
			for i := byte(0); i < tc.size; i++ {
				loadMemory.Bytes()[8+int(i)] = 0xa0 + i
			}
			want := initial
			copy(want[int(tc.lane)*int(tc.size):], loadMemory.Bytes()[8:8+int(tc.size)])
			if got := invokeV128(t, loadIn, "run", 3, lo, hi); got != want {
				t.Fatalf("load lane = % x, want % x", got, want)
			}

			storeBody := append([]byte{0x20, 0x00, 0x20, 0x01}, memory64SIMDMemOp(tc.storeSub, tc.align, 5, tc.lane)...)
			storeBody = append(storeBody, 0x0b)
			_, storeIn, storeMemory := compile(t, []wasm.ValType{wasm.I64, wasm.V128}, nil, storeBody)
			if _, err := storeIn.Invoke("run", 3, lo, hi); err != nil {
				t.Fatal(err)
			}
			laneStart := int(tc.lane) * int(tc.size)
			if got, wantBytes := storeMemory.Bytes()[8:8+int(tc.size)], initial[laneStart:laneStart+int(tc.size)]; !bytes.Equal(got, wantBytes) {
				t.Fatalf("stored lane = % x, want % x", got, wantBytes)
			}
			before := append([]byte(nil), storeMemory.Bytes()[8:8+int(tc.size)]...)
			if _, err := storeIn.Invoke("run", uint64(len(storeMemory.Bytes())-5-int(tc.size)+1), lo, hi); err == nil || !strings.Contains(err.Error(), "out of bounds") {
				t.Fatalf("store lane end trap = %v", err)
			}
			if got := storeMemory.Bytes()[8 : 8+int(tc.size)]; !bytes.Equal(got, before) {
				t.Fatalf("trapping lane store changed memory: % x", got)
			}
		})
	}
}

func memory64BulkModule() []byte {
	memory := append([]byte{0x05}, uleb64(1)...)
	memory = append(memory, uleb64(2)...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I64, wasm.I64}, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I32, wasm.I64}, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(5, wasmtest.Vec(memory)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("copy", 0, 0),
			wasmtest.ExportEntry("fill", 0, 1),
			wasmtest.ExportEntry("memory", 2, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0xfc, 0x0a, 0x00, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0xfc, 0x0b, 0x00, 0x0b}),
		)),
	)
}

func TestStagedMemory64BulkCopyFill(t *testing.T) {
	compiled, err := compileStagedMemory64(memory64BulkModule())
	if err != nil {
		t.Fatalf("compile memory64 bulk: %v", err)
	}
	defer compiled.Close()
	in, err := instantiateCore(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate memory64 bulk: %v", err)
	}
	defer in.Close()
	memory, err := in.ExportedMemory("memory")
	if err != nil {
		t.Fatal(err)
	}
	mem := memory.Bytes()
	for i := 0; i < 64; i++ {
		mem[i] = byte(i)
	}
	wantForward := append([]byte(nil), mem[0:32]...)
	if _, err := in.Invoke("copy", 8, 0, 32); err != nil {
		t.Fatalf("overlapping forward memory64.copy: %v", err)
	}
	if got := mem[8:40]; !bytes.Equal(got, wantForward) {
		t.Fatalf("overlapping forward memory64.copy = %v, want %v", got, wantForward)
	}
	for i := 0; i < 64; i++ {
		mem[i] = byte(i)
	}
	wantBackward := append([]byte(nil), mem[8:40]...)
	if _, err := in.Invoke("copy", 0, 8, 32); err != nil {
		t.Fatalf("overlapping backward memory64.copy: %v", err)
	}
	if got := mem[0:32]; !bytes.Equal(got, wantBackward) {
		t.Fatalf("overlapping backward memory64.copy = %v, want %v", got, wantBackward)
	}
	if _, err := in.Invoke("fill", 48, I32(0xab), 16); err != nil {
		t.Fatalf("memory64.fill: %v", err)
	}
	if got := mem[48:64]; !bytes.Equal(got, bytes.Repeat([]byte{0xab}, 16)) {
		t.Fatalf("memory64.fill bytes = %x", got)
	}

	for _, tc := range []struct {
		name string
		fn   string
		args []uint64
	}{
		{name: "copy destination carry", fn: "copy", args: []uint64{^uint64(0), 0, 2}},
		{name: "copy source carry", fn: "copy", args: []uint64{0, ^uint64(0), 2}},
		{name: "copy length end", fn: "copy", args: []uint64{0, 0, uint64(len(mem)) + 1}},
		{name: "fill destination carry", fn: "fill", args: []uint64{^uint64(0), I32(0xcd), 2}},
		{name: "fill length end", fn: "fill", args: []uint64{0, I32(0xcd), uint64(len(mem)) + 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := append([]byte(nil), mem[:64]...)
			if _, err := in.Invoke(tc.fn, tc.args...); err == nil || !strings.Contains(err.Error(), "out of bounds") {
				t.Fatalf("%s trap = %v", tc.fn, err)
			}
			if !bytes.Equal(mem[:64], before) {
				t.Fatalf("trapping %s changed memory", tc.fn)
			}
		})
	}
}

func TestStagedMemory64AdmissionGatesAndMemory32CodeStability(t *testing.T) {
	if _, err := compileStagedMemory64(boundedMemory64Module(65536)); err == nil || !strings.Contains(err.Error(), "65535") {
		t.Fatalf("oversized memory64 maximum error = %v", err)
	}
	shared := append([]byte{0x07}, uleb64(1)...)
	shared = append(shared, uleb64(2)...)
	sharedModule := wasmtest.Module(wasmtest.Section(5, wasmtest.Vec(shared)))
	if _, err := compileStagedMemory64(sharedModule); err == nil || !strings.Contains(err.Error(), "shared") {
		t.Fatalf("shared memory64 error = %v", err)
	}

	imported := append(wasmtest.Name("env"), wasmtest.Name("memory")...)
	imported = append(imported, byte(wasm.ExternMem), 0x05, 0x01, 0x02)
	importModule := wasmtest.Module(wasmtest.Section(2, wasmtest.Vec(imported)))
	if _, err := compileStagedMemory64(importModule); err == nil || !strings.Contains(err.Error(), "exactly one local memory") {
		t.Fatalf("imported memory64 error = %v", err)
	}

	cfg := NewRuntimeConfig()
	cfg.boundsChecks = BoundsChecksSignalsBased
	features := frontend.AllFeatures()
	features.Memory64 = true
	if _, err := compileWithFrontendFeatures(cfg, boundedMemory64Module(2), features); err == nil || !strings.Contains(err.Error(), "signals-based") {
		t.Fatalf("guard memory64 error = %v", err)
	}

	ordinary := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x01, 0x01, 0x02})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("load", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x28, 0x02, 0x00, 0x0b}))),
	)
	base, err := Compile(nil, ordinary)
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close()
	stageCfg := NewRuntimeConfig()
	stageFeatures := stageCfg.frontendFeatures()
	stageFeatures.Memory64 = true
	staged, err := compileWithFrontendFeatures(stageCfg, ordinary, stageFeatures)
	if err != nil {
		t.Fatal(err)
	}
	defer staged.Close()
	if !bytes.Equal(base.Code, staged.Code) {
		t.Fatal("enabling staged memory64 changed memory32 integer code bytes")
	}

	ordinaryFloat := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.F64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x01, 0x01, 0x02})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("load", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x2b, 0x03, 0x00, 0x0b}))),
	)
	baseFloat, err := Compile(nil, ordinaryFloat)
	if err != nil {
		t.Fatal(err)
	}
	defer baseFloat.Close()
	stagedFloat, err := compileWithFrontendFeatures(stageCfg, ordinaryFloat, stageFeatures)
	if err != nil {
		t.Fatal(err)
	}
	defer stagedFloat.Close()
	if !bytes.Equal(baseFloat.Code, stagedFloat.Code) {
		t.Fatal("enabling staged memory64 changed memory32 float code bytes")
	}

	ordinarySIMD := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.V128}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x01, 0x01, 0x02})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0xfd, 0x00, 0x04, 0x00, 0x0b}))),
	)
	baseSIMD, err := Compile(nil, ordinarySIMD)
	if err != nil {
		t.Fatal(err)
	}
	defer baseSIMD.Close()
	stagedSIMD, err := compileWithFrontendFeatures(stageCfg, ordinarySIMD, stageFeatures)
	if err != nil {
		t.Fatal(err)
	}
	defer stagedSIMD.Close()
	if !bytes.Equal(baseSIMD.Code, stagedSIMD.Code) {
		t.Fatal("enabling staged memory64 changed memory32 SIMD code bytes")
	}

	ordinaryBulk := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x01, 0x01, 0x02})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0xfc, 0x0a, 0x00, 0x00, 0x0b}))),
	)
	baseBulk, err := Compile(nil, ordinaryBulk)
	if err != nil {
		t.Fatal(err)
	}
	defer baseBulk.Close()
	stagedBulk, err := compileWithFrontendFeatures(stageCfg, ordinaryBulk, stageFeatures)
	if err != nil {
		t.Fatal(err)
	}
	defer stagedBulk.Close()
	if !bytes.Equal(baseBulk.Code, stagedBulk.Code) {
		t.Fatal("enabling staged memory64 changed memory32 bulk code bytes")
	}

	ordinaryInit := passiveDataModule()
	baseInit, err := Compile(nil, ordinaryInit)
	if err != nil {
		t.Fatal(err)
	}
	defer baseInit.Close()
	stagedInit, err := compileWithFrontendFeatures(stageCfg, ordinaryInit, stageFeatures)
	if err != nil {
		t.Fatal(err)
	}
	defer stagedInit.Close()
	if !bytes.Equal(baseInit.Code, stagedInit.Code) {
		t.Fatal("enabling staged memory64 changed memory32 memory.init/data.drop code bytes")
	}
}

func BenchmarkStagedMemory64NoMaximumSize(b *testing.B) {
	compiled, err := compileStagedMemory64(unboundedMemory64Module())
	if err != nil {
		b.Fatal(err)
	}
	defer compiled.Close()
	in, err := instantiateCore(compiled, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got, err := in.Invoke("size"); err != nil || len(got) != 1 || got[0] != 1 {
			b.Fatalf("no-max memory64.size = %v, err=%v", got, err)
		}
	}
}

func BenchmarkStagedMemory64PassiveInit(b *testing.B) {
	compiled, err := compileStagedMemory64(memory64PassiveDataModule(bytes.Repeat([]byte{0xa5}, 64)))
	if err != nil {
		b.Fatal(err)
	}
	defer compiled.Close()
	in, err := instantiateCore(compiled, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := in.Invoke("init", 64, I32(0), I32(64)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkStagedMemory64BulkFill(b *testing.B) {
	compiled, err := compileStagedMemory64(memory64BulkModule())
	if err != nil {
		b.Fatal(err)
	}
	defer compiled.Close()
	in, err := instantiateCore(compiled, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := in.Invoke("fill", 64, I32(int32(i)), 64); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkStagedMemory64SIMDLoad(b *testing.B) {
	if !hostSupportsSIMD() {
		b.Skip("host SIMD unavailable")
	}
	body := append([]byte{0x20, 0x00}, memory64SIMDMemOp(0, 4, 0)...)
	body = append(body, 0x0b)
	compiled, err := compileStagedMemory64(memory64SIMDModule([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.V128}, body))
	if err != nil {
		b.Fatal(err)
	}
	defer compiled.Close()
	in, err := instantiateCore(compiled, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := in.Invoke("run", 64); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkStagedMemory64FloatLoad(b *testing.B) {
	compiled, err := compileStagedMemory64(memory64FloatScalarModule())
	if err != nil {
		b.Fatal(err)
	}
	defer compiled.Close()
	in, err := instantiateCore(compiled, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	memory, err := in.ExportedMemory("memory")
	if err != nil {
		b.Fatal(err)
	}
	binary.LittleEndian.PutUint64(memory.Bytes()[64:], 0x7ff4000000000001)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := in.Invoke("f64.load", 64); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkStagedMemory64StoreLoad(b *testing.B) {
	compiled, err := compileStagedMemory64(boundedMemory64Module(2))
	if err != nil {
		b.Fatal(err)
	}
	defer compiled.Close()
	in, err := instantiateCore(compiled, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := in.Invoke("store_load", 8, uint64(uint32(i))); err != nil {
			b.Fatal(err)
		}
	}
}
