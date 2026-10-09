//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

func TestRV64PublicReferenceFunctionsEmptyRows(t *testing.T) {
	c := rv64NativeTailCompile(t, "reference-functions-rows", rv64ClosedReferenceSource(false, false, false, true), false)
	rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
	defer rt.Close()
	for _, field := range []string{"code", "key", "home", "slot", "context", "tail", "both"} {
		t.Run(field, func(t *testing.T) {
			in := rv64NativeTokenInstantiate(t, rt, c, NewImports())
			state := &in.pluginState.Load().railshotInvocation
			if state.definedTailOwner != nil || state.tailGenerationBudget.count != 0 {
				t.Fatal("fixture already cached an owner")
			}
			const hole = 2
			row := in.funcRefDescs[(hole+1)*coreruntime.FuncRefDescBytes : (hole+2)*coreruntime.FuncRefDescBytes]
			tail := state.tailTargets[abi.RV64TailTargetsHeaderBytes+hole*abi.RV64TailTargetsRowBytes : abi.RV64TailTargetsHeaderBytes+(hole+1)*abi.RV64TailTargetsRowBytes]
			switch field {
			case "tail":
				tail[0] = 1
			case "both":
				numeric := in.funcRefDescs[2*coreruntime.FuncRefDescBytes : 3*coreruntime.FuncRefDescBytes]
				copy(row, numeric)
				binary.LittleEndian.PutUint64(row[coreruntime.TableEntryCodePtrOffset:], uint64(in.base)+uint64(in.c.Entry[hole]))
				binary.LittleEndian.PutUint64(row[coreruntime.TableEntrySigKeyOffset:], in.c.funcTypeKey(hole))
				binary.LittleEndian.PutUint64(row[coreruntime.TableEntryRefSlotOffset:], uint64(offHeapSlicePtr(row)))
				binary.LittleEndian.PutUint64(tail, uint64(in.base)+uint64(in.c.Entry[hole]))
				binary.LittleEndian.PutUint64(tail[8:], uint64(in.jm.LinMemBase()))
				binary.LittleEndian.PutUint64(tail[16:], uint64(in.nativeContext))
			default:
				offset := map[string]int{"code": 0, "key": 8, "home": 16, "slot": 24, "context": 32}[field]
				row[offset] = 1
			}
			descriptor := uint64(offHeapSlicePtr(row))
			if _, ok := in.funcrefDescriptorIndex(descriptor); ok {
				t.Fatal("internal row acquired an index identity")
			}
			if _, ok := in.localFuncrefDescriptor(hole); ok {
				t.Fatal("internal row acquired local identity")
			}
			if _, ok := in.funcrefFunctionIdentity(descriptor); ok {
				t.Fatal("internal row acquired function identity")
			}
			if _, ok := rv64PublishedDescriptor(in, descriptor); ok {
				t.Fatal("internal row published")
			}
			in.refStore.mu.Lock()
			_, _, canonical := in.refStore.canonicalFuncrefOwnerLocked(in, descriptor)
			in.refStore.mu.Unlock()
			if canonical {
				t.Fatal("internal row acquired canonical owner")
			}
			if !in.retainResourceRoot() {
				t.Fatal("owner lease")
			}
			nativeExecutionMu.Lock()
			_, _, _, valid := rv64QualifiedNativeTailTarget(&InstanceExport{inst: in, localIdx: hole})
			nativeExecutionMu.Unlock()
			in.releaseResourceRoot()
			if valid {
				t.Fatal("internal function acquired qualified target")
			}
			// Ordinary entry has no dynamic generation or cached owner. Its live
			// directory validation must independently reject either nonempty row.
			if _, err := in.Invoke("run", 1, 0x7ff0000000000001); err == nil || !strings.Contains(err.Error(), "internal reference descriptor and tail rows") {
				t.Fatal("entry accepted internal descriptor state", err)
			}
			if state.definedTailOwner != nil || state.tailGenerationBudget.count != 0 || rv64EntryCapability(in) != 0 {
				t.Fatal("failed entry retained owner state")
			}
			clear(row)
			clear(tail)
			if got, err := in.Invoke("run", 1, 0x7ff0000000000001); err != nil || !slices.Equal(got, []uint64{0x7ff0000000000001, 6, 0}) {
				t.Fatal("restored instance reuse", got, err)
			}
			for _, index := range []int{1, 4} {
				pointer, ok := in.localFuncrefDescriptor(index)
				if !ok {
					t.Fatal("neighbor identity", index)
				}
				if _, valid := rv64PublishedDescriptor(in, pointer); !valid {
					t.Fatal("neighbor publication", index)
				}
			}
		})
	}
}

func TestRV64PublicReferenceFunctionsRetainedAdmission(t *testing.T) {
	for _, vector := range []bool{false, true} {
		c := rv64NativeTailCompile(t, "reference-functions-admission", rv64ClosedReferenceSource(vector, false, true, true), false)
		mutations := map[string]func(*Compiled){
			"missing-witness":           func(v *Compiled) { v.nativeReferenceFunctionABI = false },
			"missing-reference-feature": func(v *Compiled) { v.requiredFeatures &^= CoreFeatureReferenceTypes },
			"missing-control-witness":   func(v *Compiled) { v.nativeReferenceControlABI = false },
			"missing-typed-feature":     func(v *Compiled) { v.requiredFeatures &^= CoreFeatureTypedFunctionReferences },
			"missing-frame":             func(v *Compiled) { v.registerABIDisabled = false },
			"missing-context":           func(v *Compiled) { v.nativeContextTrapABI = false },
			"missing-table-context":     func(v *Compiled) { v.nativeTableContextABI = false },
			"missing-table-reference":   func(v *Compiled) { v.nativeTableReferenceABI = false },
			"reference-call-without-context": func(v *Compiled) {
				v.nativeReferenceCallABI = true
				v.nativeTableContextABI = false
			},
			"missing-exact-binding":   func(v *Compiled) { v.Funcs[2].HasTypeIndex = false },
			"bad-type-index":          func(v *Compiled) { v.Funcs[2].TypeIndex = 99 },
			"wrong-binding":           func(v *Compiled) { v.Funcs[2].TypeIndex = 1 },
			"wrong-abi-carrier":       func(v *Compiled) { v.Funcs[2].Params[0] = ValI64 },
			"reference-export":        func(v *Compiled) { v.Exports["hidden"] = 2 },
			"dynamic-imports":         func(v *Compiled) { v.dynamicImports = true },
			"dynamic-escape":          func(v *Compiled) { v.dynamicFuncrefEscape = true },
			"start":                   func(v *Compiled) { v.HasStart = true; v.StartLocalFunc = 2 },
			"inactive-start":          func(v *Compiled) { v.StartLocalFunc = 1 },
			"inactive-imported-start": func(v *Compiled) { v.StartImportIdx = 1 },
			"inactive-start-kind":     func(v *Compiled) { v.StartIsImport = true },
			"inactive-memory":         func(v *Compiled) { v.MemMinPages = 1 },
			"imported-memory":         func(v *Compiled) { v.HasMemory = true; v.memoryImport = "env.memory" },
			"table":                   func(v *Compiled) { v.HasTable = true },
			"inactive-table-type":     func(v *Compiled) { v.TableValueTypeIndex = 1 },
			"inactive-table-exact":    func(v *Compiled) { v.TableHasValueType = true },
			"value-pool":              func(v *Compiled) { v.ValueTypes = []ValueTypeDescriptor{{Kind: ValueTypeI32}} },
			"inactive-describes":      func(v *Compiled) { v.Types[2].Describes = 1 },
			"inactive-descriptor":     func(v *Compiled) { v.Types[2].Descriptor = 1 },
			"type-fields":             func(v *Compiled) { v.Types[2].Fields = []FieldTypeDescriptor{{}} },
			"type-array":              func(v *Compiled) { v.Types[2].Array.Mutable = true },
			"recursive-group":         func(v *Compiled) { v.Types[2].RecGroup = v.Types[1].RecGroup },
			"recursive-heap":          func(v *Compiled) { v.Types[2].Params[0].Ref.Heap.TypeIndex = 2 },
			"inactive-heap":           func(v *Compiled) { v.Types[2].Params[0].Ref.Heap.Abstract = AbstractHeapFunc },
			"exact-reference":         func(v *Compiled) { v.Types[2].Params[0].Ref.Exact = true },
			"passive-element":         func(v *Compiled) { v.passiveElems = []ElemInit{{Mode: ElemModePassive}} },
			"element-values": func(v *Compiled) {
				v.passiveElems = []ElemInit{{Mode: ElemModeDeclarative, Values: []RefInit{{FuncIndex: 1}}}}
			},
			"element-type": func(v *Compiled) { v.passiveElems = []ElemInit{{Mode: ElemModeDeclarative, ValueTypeIndex: 1}} },
			"element-offset": func(v *Compiled) {
				v.passiveElems = []ElemInit{{Mode: ElemModeDeclarative}}
				v.passiveElems[0].Offset.Base = 1
			},
		}
		for name, mutate := range mutations {
			t.Run(fmt.Sprintf("vector%t-%s", vector, name), func(t *testing.T) {
				copy := *c.executionView()
				copy.Types = cloneDefinedTypeDescriptors(copy.Types)
				copy.Funcs = slices.Clone(copy.Funcs)
				for i := range copy.Funcs {
					copy.Funcs[i].Params = slices.Clone(copy.Funcs[i].Params)
					copy.Funcs[i].Results = slices.Clone(copy.Funcs[i].Results)
				}
				copy.Exports = maps.Clone(copy.Exports)
				copy.passiveElems = slices.Clone(copy.passiveElems)
				mutate(&copy)
				if err := copy.validate(); err == nil {
					t.Fatal("malformed closed metadata accepted")
				}
			})
		}
		// Persisted malformed shape must also fail loading. Inactive union fields
		// and inferred feature bits have canonicalizing writers, so test those
		// directly above instead of assuming the codec preserves them.
		for _, name := range []string{"missing-witness", "missing-control-witness", "missing-frame", "missing-context", "missing-table-context", "missing-table-reference", "reference-call-without-context", "bad-type-index", "wrong-binding", "wrong-abi-carrier", "reference-export", "dynamic-imports", "dynamic-escape", "start", "imported-memory", "table", "value-pool", "type-fields", "type-array", "recursive-group", "recursive-heap", "exact-reference", "passive-element", "element-values"} {
			t.Run(fmt.Sprintf("vector%t-codec-%s", vector, name), func(t *testing.T) {
				copy := *c.executionView()
				copy.Types = cloneDefinedTypeDescriptors(copy.Types)
				copy.Funcs = slices.Clone(copy.Funcs)
				for i := range copy.Funcs {
					copy.Funcs[i].Params = slices.Clone(copy.Funcs[i].Params)
					copy.Funcs[i].Results = slices.Clone(copy.Funcs[i].Results)
				}
				copy.Exports = maps.Clone(copy.Exports)
				copy.passiveElems = slices.Clone(copy.passiveElems)
				mutations[name](&copy)
				blob, err := marshalCompiled(&copy)
				if err != nil {
					return
				}
				loaded, err := LoadTrustedArtifact(blob)
				if loaded != nil {
					loaded.Close()
				}
				if err == nil {
					t.Fatal("malformed closed artifact loaded")
				}
			})
		}
	}
	c := rv64NativeTailCompile(t, "reference-functions-spurious", rv64PublicNativeIdentityModule(nil), false)
	copy := *c.executionView()
	copy.nativeReferenceFunctionABI = true
	copy.requiredFeatures |= CoreFeatureReferenceTypes
	if err := copy.validate(); err == nil {
		t.Fatal("numeric image accepted closed witness")
	}
	// No additional directory or body bytes are serialized with the new bit.
	plain, err := marshalCompiledMetadata(c)
	if err != nil {
		t.Fatal(err)
	}
	flagged, err := marshalCompiledMetadata(&copy)
	if err != nil {
		t.Fatal(err)
	}
	if len(plain) != len(flagged) || bytes.Equal(plain, flagged) {
		t.Fatal("closed witness footprint")
	}
}
