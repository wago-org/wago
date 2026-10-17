//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"fmt"
	"maps"
	"slices"
	"testing"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

func rv64RequirePublishedLocalFunction(t *testing.T, in *Instance, index int) {
	t.Helper()
	descriptor, ok := in.localFuncrefDescriptor(index)
	if !ok {
		t.Fatal("local function descriptor", index)
	}
	function, ok := in.funcrefDescriptorIndex(descriptor)
	if !ok || function != in.c.NumImports+index {
		t.Fatal("function descriptor index", index, function)
	}
	identity, ok := in.funcrefFunctionIdentity(descriptor)
	if !ok || identity.instance != in || identity.localIdx != index {
		t.Fatal("function descriptor identity", index, identity)
	}
	if _, ok := rv64PublishedDescriptor(in, descriptor); !ok {
		t.Fatal("function descriptor publication", index)
	}
}

func TestRV64PublicReferenceFunctionsCorruptRows(t *testing.T) {
	c := rv64NativeTailCompile(t, "reference-functions-rows", rv64ClosedReferenceSource(false, false, false, true), false)
	rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
	defer rt.Close()
	for _, field := range []string{"code", "key", "home", "slot", "context", "tail", "both"} {
		t.Run(field, func(t *testing.T) {
			in := rv64NativeTokenInstantiate(t, rt, c, NewImports())
			defer in.Close()
			const index = 2
			rv64RequirePublishedLocalFunction(t, in, index)
			row := in.funcRefDescs[(index+1)*coreruntime.FuncRefDescBytes : (index+2)*coreruntime.FuncRefDescBytes]
			state := &in.pluginState.Load().railshotInvocation
			tail := state.tailTargets[abi.RV64TailTargetsHeaderBytes+index*abi.RV64TailTargetsRowBytes : abi.RV64TailTargetsHeaderBytes+(index+1)*abi.RV64TailTargetsRowBytes]
			savedRow, savedTail := bytes.Clone(row), bytes.Clone(tail)
			if field == "tail" || field == "both" {
				tail[0] ^= 1
			}
			if field != "tail" {
				offset := map[string]int{"code": 0, "key": 8, "home": 16, "slot": 24, "context": 32, "both": 0}[field]
				row[offset] ^= 1
				descriptor := uint64(offHeapSlicePtr(row))
				if _, ok := rv64PublishedDescriptor(in, descriptor); ok {
					t.Fatal("corrupt descriptor published")
				}
				in.refStore.mu.Lock()
				_, _, canonical := in.refStore.canonicalFuncrefOwnerLocked(in, descriptor)
				in.refStore.mu.Unlock()
				if canonical {
					t.Fatal("corrupt descriptor acquired canonical owner")
				}
			}
			if field == "tail" || field == "both" {
				if _, err := in.Invoke("run", 1, 0x7ff0000000000001); err == nil {
					t.Fatal("entry accepted corrupt tail row")
				}
			}
			if state.definedTailOwner != nil || state.tailGenerationBudget.count != 0 || rv64EntryCapability(in) != 0 {
				t.Fatal("failed entry retained owner state")
			}
			copy(row, savedRow)
			copy(tail, savedTail)
			if got, err := in.Invoke("run", 1, 0x7ff0000000000001); err != nil || !slices.Equal(got, []uint64{0x7ff0000000000001, 6, 0}) {
				t.Fatal("restored instance reuse", got, err)
			}
			for index := range c.Funcs {
				rv64RequirePublishedLocalFunction(t, in, index)
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
			"bad-export":              func(v *Compiled) { v.Exports["hidden"] = 99 },
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
				copy := *rv64ReferenceImportTailCopy(c)
				copy.Types = cloneDefinedTypeDescriptors(copy.Types)
				copy.Funcs = slices.Clone(copy.Funcs)
				for i := range copy.Funcs {
					copy.Funcs[i].Params = slices.Clone(copy.Funcs[i].Params)
					copy.Funcs[i].Results = slices.Clone(copy.Funcs[i].Results)
				}
				copy.Exports = maps.Clone(copy.Exports)
				copy.passiveElems = slices.Clone(copy.passiveElems)
				mutate(&copy)
				if name == "inactive-table-type" || name == "inactive-describes" || name == "inactive-descriptor" {
					rv64RequireTableArtifactMetadata(t, &copy)
					return
				}
				if err := copy.validate(); err == nil {
					t.Fatal("malformed closed metadata accepted")
				}
			})
		}
		// Persisted malformed shape must also fail loading. Inactive union fields
		// and inferred feature bits have canonicalizing writers, so test those
		// directly above instead of assuming the codec preserves them.
		for _, name := range []string{"missing-witness", "missing-control-witness", "missing-frame", "missing-context", "missing-table-context", "missing-table-reference", "reference-call-without-context", "bad-type-index", "wrong-binding", "wrong-abi-carrier", "bad-export", "dynamic-imports", "dynamic-escape", "start", "imported-memory", "table", "value-pool", "type-fields", "type-array", "recursive-group", "recursive-heap", "exact-reference", "passive-element", "element-values"} {
			t.Run(fmt.Sprintf("vector%t-codec-%s", vector, name), func(t *testing.T) {
				copy := *rv64ReferenceImportTailCopy(c)
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
	copy := *rv64ReferenceImportTailCopy(c)
	copy.nativeReferenceFunctionABI = true
	copy.requiredFeatures |= CoreFeatureReferenceTypes
	plainReference, ok := valueTypeDescriptorFromValType(ValFuncRef)
	if !ok {
		t.Fatal("plain funcref exact type")
	}
	copy.ValueTypes = []ValueTypeDescriptor{plainReference}
	copy.Globals = []GlobalDef{{Type: ValFuncRef, HasValueType: true}}
	if err := copy.validate(); err == nil {
		t.Fatal("reference global manufactured a closed function witness")
	}
	blob, err := marshalCompiled(&copy)
	if err == nil {
		loaded, loadErr := LoadTrustedArtifact(blob)
		if loaded != nil {
			_ = loaded.Close()
		}
		if loadErr == nil {
			t.Fatal("reference-global-only artifact manufactured a closed function witness")
		}
	}
}

func rv64RequireCachedValidationNoAllocations(t *testing.T, c *Compiled) {
	t.Helper()
	view := c.executionView()
	if err := view.validateCached(); err != nil {
		t.Fatal(err)
	}
	if count := testing.AllocsPerRun(10, func() {
		if err := view.validateCached(); err != nil {
			panic(err)
		}
	}); count != 0 {
		t.Fatal("cached execution validation allocations", count)
	}
}

func rv64RequireRejectedArtifact(t *testing.T, c *Compiled) {
	t.Helper()
	blob, err := marshalCompiled(c)
	if err != nil {
		return
	}
	loaded, err := LoadTrustedArtifact(blob)
	if loaded != nil {
		loaded.Close()
	}
	if err == nil {
		t.Fatal("invalid execution artifact loaded")
	}
}

func rv64RequireRestoredGCFeature(t *testing.T, c *Compiled) {
	t.Helper()
	blob, err := marshalCompiled(c)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadTrustedArtifact(blob)
	if err != nil {
		t.Fatal(err)
	}
	defer loaded.Close()
	if !loaded.requiredFeatures.IsEnabled(CoreFeatureGC) {
		t.Fatal("codec did not restore the inferred GC feature")
	}
}
