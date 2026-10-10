//go:build !riscv64

package wago

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func railshotAdmitSource([]byte, *wasm.Module) error                                    { return nil }
func railshotValidateCompiled(*Compiled) error                                          { return nil }
func railshotValidateResolvedImports(*Compiled, resolvedImports, *referenceStore) error { return nil }
func railshotNeedsImportContextHeader(int) bool                                         { return false }

func railshotPublishConstructedTableProducer(*Table, *Instance) *Instance { return nil }

func railshotLockNativeTableScan()   {}
func railshotUnlockNativeTableScan() {}

func railshotValidateReferenceCalls(c *Compiled) error {
	if c.nativeReferenceCallABI || c.nativeReferenceControlABI || c.nativeReferenceFunctionABI || c.nativeTable64SizeABI {
		return fmt.Errorf("RV64 typed-reference execution on a foreign architecture")
	}
	return nil
}
