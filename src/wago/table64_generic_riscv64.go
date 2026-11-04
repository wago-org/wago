//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import "fmt"

// Generic table64 uses the retained indexed descriptor ABI and the complete
// native execution binding. The older operation-specific witnesses remain
// limited to their exact images and metadata.
func rv64GenericTable64(c *Compiled) bool {
	return c != nil && c.nativeTable64ABI == nativeTable64NoABI && c.requiredFeatures.IsEnabled(CoreFeatureTable64) && rv64SourceContractValid(c)
}

func rv64ValidateGenericTable64(c *Compiled) error {
	if !c.nativeTableContextABI || !c.nativeTableReferenceABI {
		return fmt.Errorf("generic table64 requires retained native descriptors")
	}
	function, _ := valueTypeDescriptorFromValType(ValFuncRef)
	external, _ := valueTypeDescriptorFromValType(ValExternRef)
	has64 := false
	for index := 0; index < c.tableCount(); index++ {
		exact, err := c.tableExactType(index)
		if err != nil || exact != function && exact != external && !rv64PrimitiveReferenceValue(c, exact) {
			return fmt.Errorf("generic table64 %d requires a validated function or external table type", index)
		}
		has64 = has64 || c.tableDef(index).Addr64
		if imported, ok := c.tableImportAt(index); ok && imported.Min > uint64(^uint32(0)) {
			return fmt.Errorf("generic table64 import %d exceeds native size representation", index)
		}
	}
	if !has64 {
		return fmt.Errorf("generic table64 requires a declared 64-bit table")
	}
	return rv64ValidatePlainReferenceTableDirectory(c, true)
}
