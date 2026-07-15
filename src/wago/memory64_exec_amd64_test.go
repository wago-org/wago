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

func boundedMemory64Module(max uint64) []byte {
	mem := append([]byte{0x05}, uleb64(1)...)
	mem = append(mem, uleb64(max)...)
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

func memory64FloatLoadModule() []byte {
	memory := append([]byte{0x05}, uleb64(1)...)
	memory = append(memory, uleb64(2)...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.F32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec(memory)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x2a, 0x02, 0x00, 0x0b}))),
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

func TestStagedMemory64AdmissionGatesAndMemory32CodeStability(t *testing.T) {
	if _, err := compileStagedMemory64(boundedMemory64Module(65536)); err == nil || !strings.Contains(err.Error(), "65535") {
		t.Fatalf("oversized memory64 maximum error = %v", err)
	}
	if _, err := compileStagedMemory64(memory64FloatLoadModule()); err == nil || !strings.Contains(err.Error(), "outside staged integer scalar family") {
		t.Fatalf("floating-point memory64 gate error = %v", err)
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
		t.Fatal("enabling staged memory64 changed memory32 code bytes")
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
