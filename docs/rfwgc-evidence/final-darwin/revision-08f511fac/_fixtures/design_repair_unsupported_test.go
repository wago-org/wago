//go:build darwin && arm64 && !tinygo && !wago_target_tinygo

package wago

import "testing"

// Harness-only migration: unsupported adapter gates now require positive ownership.
func TestDesignRepairUnsupportedConvertedPublication(t *testing.T) {
	t.Run("TestR9H2HostContainerPublicGlobal", TestR9H2HostContainerPublicGlobal)
	t.Run("TestR9H2HostContainerPublicTable", TestR9H2HostContainerPublicTable)
}
func TestDesignRepairUnsupportedBulkAtomicity(t *testing.T) {
	t.Run("TestR9H2StoreHooksBulk", TestR9H2StoreHooksBulk)
	t.Run("TestR9H2StoreHooksActiveInitialization", TestR9H2StoreHooksActiveInitialization)
	t.Run("TestR9H2ReceiptIndependentContainersAndRollback", TestR9H2ReceiptIndependentContainersAndRollback)
}
