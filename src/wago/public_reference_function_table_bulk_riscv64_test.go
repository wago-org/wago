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

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Move every numeric export body into a private function with an additional
// reference parameter. The public numeric wrapper supplies null. Original
// numeric descriptor and element ordinals stay unchanged. These fixtures have
// no declared locals, so appending the parameter preserves all local indices.
func rv64ReferenceTableBulkPrivateModule(t *testing.T, source []byte, vector bool) []byte {
	t.Helper()
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	types, functions, bodies := [][]byte{}, [][]byte{}, [][]byte{}
	for _, group := range m.Types {
		if len(group.SubTypes) != 1 {
			t.Fatal("fixture recursive types")
		}
		sig := group.SubTypes[0].Comp
		types = append(types, wasmtest.FuncType(sig.Params, sig.Results))
	}
	for i, fn := range m.Code {
		if len(fn.Locals.Runs) != 0 {
			t.Fatal("fixture local-index assumption")
		}
		functions = append(functions, wasmtest.ULEB(m.FuncTypes[i].Index))
		bodies = append(bodies, wasmtest.Code(fn.BodyBytes))
	}
	for _, ex := range m.Exports {
		if ex.Index.Kind != wasm.ExternFunc {
			continue
		}
		index := int(ex.Index.Index)
		sig := m.Types[m.FuncTypes[index].Index].SubTypes[0].Comp
		params := append(slices.Clone(sig.Params), wasm.FuncRef)
		privateIndex := len(functions)
		functions = append(functions, wasmtest.ULEB(uint32(len(types))))
		types = append(types, wasmtest.FuncType(params, sig.Results))
		bodies = append(bodies, bodies[index])
		wrapper := []byte{}
		for param := range sig.Params {
			wrapper = append(wrapper, 0x20)
			wrapper = append(wrapper, wasmtest.ULEB(uint32(param))...)
		}
		wrapper = append(wrapper, 0xd0, 0x70, 0x10)
		wrapper = append(wrapper, wasmtest.ULEB(uint32(privateIndex))...)
		wrapper = append(wrapper, 0x0b)
		bodies[index] = wasmtest.Code(wrapper)
	}
	if vector {
		functions = append(functions, wasmtest.ULEB(uint32(len(types))))
		types = append(types, wasmtest.FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128}))
		bodies = append(bodies, wasmtest.Code([]byte{0x20, 0, 0x0b}))
	}
	var sections [][]byte
	r := wasm.ReaderFrom(source[8:])
	for r.HasNext() {
		id, err := r.Byte()
		if err != nil {
			t.Fatal(err)
		}
		n, err := r.U32()
		if err != nil {
			t.Fatal(err)
		}
		payload, err := r.Bytes(int(n))
		if err != nil {
			t.Fatal(err)
		}
		switch id {
		case 1:
			payload = wasmtest.Vec(types...)
		case 3:
			payload = wasmtest.Vec(functions...)
		case 10:
			payload = wasmtest.Vec(bodies...)
		}
		sections = append(sections, wasmtest.Section(id, payload))
	}
	return wasmtest.Module(sections...)
}

func rv64ReferenceTableBulkCompile(t *testing.T, name string, source []byte, vector, artifact bool) *Compiled {
	t.Helper()
	c := rv64NativeTailCompile(t, fmt.Sprintf("reference-table-bulk-%s-v%t", name, vector), rv64ReferenceTableBulkPrivateModule(t, source, vector), artifact)
	if !c.nativeReferenceFunctionABI || !c.nativeTableReferenceABI || !c.nativeTableContextABI {
		t.Fatal("private table bulk profile not selected")
	}
	return c
}
func rv64ReferenceTableBulkInstantiate(t *testing.T, c *Compiled) *Instance {
	t.Helper()
	rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
	t.Cleanup(func() { rv64HostCertificateCloseRuntime(t, rt) })
	return rv64NativeTokenInstantiate(t, rt, c, NewImports())
}

func TestRV64PublicReferenceFunctionTableBulkElements(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			t.Run(fmt.Sprintf("artifact%t-vector%t", artifact, vector), func(t *testing.T) {
				c := rv64ReferenceTableBulkCompile(t, "elements", privateTableElementsModule(), vector, artifact)
				rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
				defer rv64HostCertificateCloseRuntime(t, rt)
				first := rv64NativeTokenInstantiate(t, rt, c, NewImports())
				// A second instance in the same runtime has an independent drop directory.
				second := rv64NativeTokenInstantiate(t, rt, c, NewImports())
				c.Close()
				runtime.GC()
				if len(first.c.passiveElems) != 256 || first.c.passiveElems[0].Mode != ElemModeActive || first.c.passiveElems[1].Mode != ElemModeDeclarative || len(first.c.passiveElems[255].Values) != 1 {
					t.Fatal("original mixed segment ordinals")
				}
				importedGlobalInvoke(t, first, "init-passive", nil, 0xfeedface00000001, 0xdeadbeef00000000, 0xcafebabe00000002)
				assertPrivateTableTargets(t, first, 12, 14, 12, 0)
				importedGlobalInvoke(t, first, "init-last", nil, 3, 0, 1)
				assertPrivateTableTargets(t, first, 12, 14, 12, 14)
				directory := unsafe.Slice((*byte)(offHeapPtr(first.jm.PassiveElemPtr())), 256*16)
				pointer := binary.LittleEndian.Uint64(directory[2*16:])
				binary.LittleEndian.PutUint32(directory[2*16+12:], 0x11223344)
				importedGlobalInvoke(t, first, "drop-passive", nil)
				importedGlobalInvoke(t, first, "drop-passive", nil)
				if binary.LittleEndian.Uint64(directory[2*16:]) != pointer || binary.LittleEndian.Uint32(directory[2*16+8:]) != 0 || binary.LittleEndian.Uint32(directory[2*16+12:]) != 0x11223344 {
					t.Fatal("drop touched pointer/padding")
				}
				importedGlobalInvoke(t, first, "init-passive", nil, 4, 0, 0)
				before := slices.Clone(privateTableBytes(first))
				for _, args := range [][]uint64{{0, 1, 0}, {0, 0, 1}, {5, 0, 0}} {
					trap := ownedTableTrap(t, invokePrivateReference(t, first, "init-passive", args...), coreruntime.TrapTableOutOfBounds)
					if len(trap.Frames) == 0 || trap.Frames[0].FunctionIndex != 16 || !trap.Frames[0].HasProgramCounter || trap.Frames[0].ProgramCounter != 7 {
						t.Fatal("private init source", trap.Frames)
					}
					if !slices.Equal(before, privateTableBytes(first)) {
						t.Fatal("failed range wrote table")
					}
				}
				importedGlobalInvoke(t, second, "init-passive", nil, 1, 0, 2)
				assertPrivateTableTargets(t, second, 12, 14, 12, 0)
				importedGlobalInvoke(t, first, "drop-last", nil)
				ownedTableTrap(t, invokePrivateReference(t, first, "init-last", 3, 0, 1), coreruntime.TrapTableOutOfBounds)
				importedGlobalInvoke(t, second, "init-last", nil, 3, 0, 1)
				fn, err := second.WasmFunc("init-last")
				if err != nil {
					t.Fatal(err)
				}
				if n := testing.AllocsPerRun(20, func() {
					if _, err := fn.Invoke(3, 0, 1); err != nil {
						panic(err)
					}
				}); n != 0 {
					t.Fatal("warm private init allocation", n)
				}
				if err := first.Close(); err != nil {
					t.Fatal(err)
				}
				assertPrivateTableTargets(t, second, 12, 14, 12, 14)
				if err := second.Close(); err != nil || second.hasPhysicalResources() {
					t.Fatal("final passive resources", err)
				}
			})
		}
	}
}

func TestRV64PublicReferenceFunctionTableBulkRangesAndGrowth(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			for _, hasMax := range []bool{false, true} {
				t.Run(fmt.Sprintf("artifact%t-vector%t-max%t", artifact, vector, hasMax), func(t *testing.T) {
					c := rv64ReferenceTableBulkCompile(t, fmt.Sprintf("ranges-max%t", hasMax), privateTableBulkModule(hasMax), vector, artifact)
					in := rv64ReferenceTableBulkInstantiate(t, c)
					before := slices.Clone(privateTableBytes(in))
					for _, tc := range []struct {
						name string
						args []uint64
					}{{"fill-a", []uint64{3, 2}}, {"fill-null", []uint64{0, 0xffffffff}}, {"copy", []uint64{3, 0, 2}}, {"copy", []uint64{0, 3, 2}}, {"copy", []uint64{0, 5, 0}}, {"copy", []uint64{5, 0, 0}}} {
						trap := ownedTableTrap(t, invokePrivateReference(t, in, tc.name, tc.args...), coreruntime.TrapTableOutOfBounds)
						wantFunction := map[string]uint32{"fill-a": 13, "fill-null": 15, "copy": 16}[tc.name]
						if len(trap.Frames) == 0 || trap.Frames[0].FunctionIndex != wantFunction || !trap.Frames[0].HasProgramCounter || trap.Frames[0].ProgramCounter != 7 {
							t.Fatal("private bulk source", trap.Frames)
						}
						if !slices.Equal(before, privateTableBytes(in)) {
							t.Fatal("range failure wrote table")
						}
					}
					importedGlobalInvoke(t, in, "fill-a", nil, 4, 0)
					importedGlobalInvoke(t, in, "copy", nil, 4, 4, 0)
					importedGlobalInvoke(t, in, "fill-b", nil, 0xfeedface00000003, 0xdeadbeef00000001)
					assertPrivateTableTargets(t, in, 12, 14, 0, 14)
					importedGlobalInvoke(t, in, "copy", nil, 0xfeedface00000001, 0xdeadbeef00000000, 0xcafebabe00000003)
					assertPrivateTableTargets(t, in, 12, 12, 14, 0)
					importedGlobalInvoke(t, in, "copy", nil, 0, 1, 3)
					assertPrivateTableTargets(t, in, 12, 14, 0, 0)
					importedGlobalInvoke(t, in, "fill-null", nil, 1, 3)
					if !bytes.Equal(privateTableBytes(in)[40:136], make([]byte, 96)) {
						t.Fatal("partial null rows")
					}
					before = slices.Clone(privateTableBytes(in))
					importedGlobalInvoke(t, in, "grow-a", []uint64{0xffffffff}, 0xcafebabeffffffff)
					if !slices.Equal(before, privateTableBytes(in)) {
						t.Fatal("failed growth wrote storage")
					}
					importedGlobalInvoke(t, in, "grow-a", []uint64{4}, 0xfeedface00000002)
					importedGlobalInvoke(t, in, "size", []uint64{6})
					assertPrivateTableTargets(t, in, 12, 0, 0, 0, 12, 12)
					limit := uint64(8)
					if !hasMax {
						limit = 1024
					}
					importedGlobalInvoke(t, in, "grow-null", []uint64{6}, limit-6)
					importedGlobalInvoke(t, in, "size", []uint64{limit})
					before = slices.Clone(privateTableBytes(in))
					importedGlobalInvoke(t, in, "grow-a", []uint64{0xffffffff}, 1)
					if !slices.Equal(before, privateTableBytes(in)) {
						t.Fatal("capacity failure wrote storage")
					}
					fn, err := in.WasmFunc("fill-a")
					if err != nil {
						t.Fatal(err)
					}
					if n := testing.AllocsPerRun(20, func() {
						if _, err := fn.Invoke(0, limit); err != nil {
							panic(err)
						}
					}); n != 0 {
						t.Fatal("warm private bulk allocation", n)
					}
				})
			}
		}
	}
}

func TestRV64PublicReferenceFunctionTableBulkRetainedBounds(t *testing.T) {
	c := rv64ReferenceTableBulkCompile(t, "retained", privateTableElementsModule(), false, false)
	for name, mutate := range map[string]func(*Compiled){
		"private": func(v *Compiled) { v.passiveElems[2].Values[0].FuncIndex = 13 }, "missing": func(v *Compiled) { v.passiveElems[2].Values[0].FuncIndex = 127 },
		"null": func(v *Compiled) { v.passiveElems[2].Values[0].Null = true }, "global": func(v *Compiled) { v.passiveElems[2].Values[0].HasGlobal = true }, "inactive-global": func(v *Compiled) { v.passiveElems[2].Values[0].GlobalIndex = 1 },
		"i31": func(v *Compiled) { v.passiveElems[2].Values[0].I31Wrap = true }, "repeat": func(v *Compiled) { v.passiveElems[2].Values[0].RepeatPrevious = true }, "expression": func(v *Compiled) { v.passiveElems[2].Values[0].Expr = []byte{0xd2, 0, 0x0b} },
		"offset": func(v *Compiled) { v.passiveElems[2].Offset.Base = 1 }, "table": func(v *Compiled) { v.passiveElems[2].TableIndex = 1 }, "typed-unbound": func(v *Compiled) {
			v.passiveElems[2].HasValueType = true
			v.passiveElems[2].ValueTypeIndex = uint32(len(v.ValueTypes))
		}, "count": func(v *Compiled) { v.passiveElems = append(v.passiveElems, ElemInit{}) },
		"combined1025": func(v *Compiled) { v.passiveElems[2].Values = make([]RefInit, 1023) },
	} {
		t.Run(name, func(t *testing.T) {
			v := cloneCompiledMetadata(c)
			v.codeCache = nil
			v.validateMemo = &validateMemo{}
			mutate(v)
			if err := railshotValidateCompiled(v); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("invalid passive metadata", err)
			}
			blob, err := marshalCompiled(v)
			if err != nil {
				return
			}
			loaded, err := LoadTrustedArtifact(blob)
			if loaded != nil {
				defer loaded.Close()
			}
			if err == nil {
				// The canonical writer drops fields inactive for legacy numeric
				// literals. Direct invalid metadata must fail, while its repaired
				// artifact must contain the original canonical numeric value.
				repaired := name == "i31" || name == "inactive-global"
				value := loaded.passiveElems[2].Values[0]
				if !repaired || value.FuncIndex != 1 || value.GlobalIndex != 0 || value.I31Wrap || rv64ValidateReferenceFunctions(loaded) != nil {
					t.Fatal("invalid passive artifact", name)
				}
			}
		})
	}
	v := cloneCompiledMetadata(c)
	v.codeCache = nil
	v.validateMemo = &validateMemo{}
	v.passiveElems[2].Values = make([]RefInit, 1022)
	if err := rv64ValidateReferenceFunctions(v); err != nil {
		t.Fatal("combined active/passive1024", err)
	}
	// Table21 + active24 + empty active20 + original directory256*16 +
	// three passive values12 =4173 bytes.
	for _, budget := range []int{4173, 4172} {
		remaining := budget
		if err := rv64ValidateReferenceFunctionTable(c, &remaining); (err == nil) != (budget == 4173) {
			t.Fatal("independent table/active/passive byte budget", budget, err)
		}
	}
	if n := testing.AllocsPerRun(5, func() {
		budget := railshot.MaxModuleBytes
		if err := rv64ValidateReferenceFunctionTable(c, &budget); err != nil {
			panic(err)
		}
	}); n != 0 {
		t.Fatal("passive shape proof allocation", n)
	}
}

func TestRV64PublicReferenceFunctionTableBulkAggregateBoundary(t *testing.T) {
	c := rv64NativeTailCompile(t, "reference-table-bulk-budget", rv64ReferenceOwnedTableLiteralModule(false, []byte{0xd2, 0, 0x0b}, 1, 1, false, false), false)
	for _, extra := range []int{0, 1} {
		t.Run(fmt.Sprintf("extra%d", extra), func(t *testing.T) {
			v := cloneCompiledMetadata(c)
			v.codeCache = nil
			v.validateMemo = &validateMemo{}
			v.Globals = []GlobalDef{{Type: ValFuncRef, Mutable: true, HasValueType: true, ValueTypeIndex: 0}}
			v.Elems = []ElemInit{{Mode: ElemModeActive, RefType: ValFuncRef, Values: []RefInit{{FuncIndex: 0}}}}
			v.passiveElems = []ElemInit{{Mode: ElemModeActive, RefType: ValFuncRef}, {Mode: ElemModePassive, RefType: ValFuncRef, Values: []RefInit{{FuncIndex: 0}, {FuncIndex: 0}, {FuncIndex: 0}}}}
			v.requiredFeatures |= CoreFeatureBulkMemoryOperations
			// Table26 + active24 + dropped16 + passive28 + reference global18 +
			// declared carriers14 + export name8 + passive data header4 =138 bytes.
			v.PassiveData = []PassiveDataInit{{Bytes: make([]byte, railshot.MaxModuleBytes-138+extra)}}
			if err := rv64ValidateReferenceFunctions(v); (err == nil) != (extra == 0) {
				t.Fatal("complete retained state/passive aggregate", extra, err)
			}
		})
	}
}

func TestRV64PublicReferenceFunctionTableBulkCancellation(t *testing.T) {
	for _, vector := range []bool{false, true} {
		t.Run(fmt.Sprintf("vector%t", vector), func(t *testing.T) {
			t.Run("bulk", func(t *testing.T) { rv64PrivateTableBulkCancellation(t, true, vector) })
			t.Run("init", func(t *testing.T) { rv64PrivateTableInitCancellation(t, true, vector) })
		})
	}
}

func TestRV64PublicReferenceFunctionTableBulkReservation(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			for _, minimum := range []uint32{512, 513} {
				t.Run(fmt.Sprintf("artifact%t-vector%t-min%d", artifact, vector, minimum), func(t *testing.T) {
					c := rv64ReferenceTableBulkCompile(t, fmt.Sprintf("reserve-min%d", minimum), privateTableBulkSizedModule(minimum == 513, minimum, 1024), vector, artifact)
					in := rv64ReferenceTableBulkInstantiate(t, c)
					importedGlobalInvoke(t, in, "grow-a", []uint64{uint64(minimum)}, 0xfeedface00000000|uint64(1024-minimum))
					importedGlobalInvoke(t, in, "size", []uint64{1024})
					importedGlobalInvoke(t, in, "call", []uint64{12}, 1023, 5)
					row := in.funcRefDescs[40:72]
					table := privateTableBytes(in)
					for index := minimum; index < 1024; index++ {
						if !slices.Equal(table[8+index*32:8+(index+1)*32], row) {
							t.Fatal("reserved growth row", index)
						}
					}
				})
			}
		}
	}
}

func TestRV64PublicReferenceFunctionTableBulkFeatureGates(t *testing.T) {
	source := rv64ReferenceTableBulkPrivateModule(t, privateTableElementsModule(), false)
	c, err := Compile(rv64NativeTailConfig().WithFeature(CoreFeatureBulkMemoryOperations, false), source)
	if c != nil || err == nil {
		if c != nil {
			c.Close()
		}
		t.Fatal("private bulk source feature gate", err)
	}
	c = rv64NativeTailCompile(t, "reference-table-bulk-feature-gates", source, false)
	if c.requiredFeatures&CoreFeatureBulkMemoryOperations == 0 {
		t.Fatal("missing bulk witness")
	}
	blob, err := c.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadTrustedArtifact(blob)
	if err != nil {
		t.Fatal(err)
	}
	defer loaded.Close()
	// Core feature selection governs compilation. A loaded private artifact
	// instead needs the existing explicit private-function destination authority.
	cache := loaded.loadCodeCache()
	beforeBase, beforeMem, beforeRefs, beforeFlags := cache.base, offHeapSlicePtr(cache.mem), cache.refs, cache.flags
	v := cloneCompiledMetadata(loaded)
	v.codeCache = nil
	v.validateMemo = &validateMemo{}
	v.requiredFeatures &^= CoreFeatureBulkMemoryOperations
	if err := railshotValidateCompiled(v); !errors.Is(err, railshot.ErrUnsupported) {
		t.Fatal("missing retained bulk witness", err)
	}
	rt := NewRuntime()
	defer rv64HostCertificateCloseRuntime(t, rt)
	mod, err := rt.Module(loaded)
	if err == nil {
		defer mod.Close()
		in, instantiateErr := rt.Instantiate(context.Background(), mod)
		if in != nil {
			in.Close()
		}
		if in != nil || instantiateErr == nil {
			t.Fatal("private bulk destination feature gate", instantiateErr)
		}
	}
	if cache.base != beforeBase || offHeapSlicePtr(cache.mem) != beforeMem || cache.sealed || cache.refs != beforeRefs || cache.flags != beforeFlags {
		t.Fatal("rejected private destination changed/published staging")
	}
}
