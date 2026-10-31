//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
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
