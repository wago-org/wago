//go:build !riscv64

package wago

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func railshotPrimitiveReferenceDeclarations(*Compiled) bool { return false }
func railshotPrimitiveReferenceContract(*Compiled) bool     { return false }

func railshotGeneralI31Module(*wasm.Module) bool { return false }

func railshotValidateCompiled(*Compiled) error                                          { return nil }
func railshotValidateResolvedImports(*Compiled, resolvedImports, *referenceStore) error { return nil }

func railshotPublishConstructedTableProducer(*Table, *Instance) *Instance { return nil }

func railshotLockNativeTableScan()   {}
func railshotUnlockNativeTableScan() {}

func railshotValidateReferenceCalls(c *Compiled) error {
	if c.nativeReferenceCallABI || c.nativeReferenceControlABI || c.nativeReferenceFunctionABI || c.nativeTable64ABI != nativeTable64NoABI {
		return fmt.Errorf("RV64 typed-reference execution on a foreign architecture")
	}
	return nil
}
