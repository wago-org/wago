package wago

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func moduleSyncHostSlotCapacity(m *wasm.Module) (int, error) {
	maxSlots := coreruntime.MaxHostArity
	if m == nil {
		return maxSlots, nil
	}
	functionIndex := 0
	for importIndex := range m.Imports {
		if m.Imports[importIndex].Type.Kind != wasm.ExternFunc {
			continue
		}
		ft, ok := m.ImportFuncType(importIndex)
		if !ok {
			return 0, fmt.Errorf("imported function %d signature is unavailable", functionIndex)
		}
		for _, values := range [][]wasm.ValType{ft.Params, ft.Results} {
			slots := 0
			for _, value := range values {
				slots++
				if value == wasm.V128 {
					slots++
				}
				if slots > coreruntime.MaxSyncHostSlots {
					return 0, &coreruntime.ImplementationLimitError{
						Feature: "synchronous host-call signature",
						Shape:   fmt.Sprintf("imported function %d requires more than %d ABI slots", functionIndex, coreruntime.MaxSyncHostSlots),
						Limit:   coreruntime.MaxSyncHostSlots,
					}
				}
			}
			if slots > maxSlots {
				maxSlots = slots
			}
		}
		functionIndex++
	}
	return maxSlots, nil
}
