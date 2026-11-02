//go:build darwin && arm64 && !tinygo && !wago_target_tinygo

package wago

import "testing"

// Harness-only migration: unsupported adapter gates now require positive ownership.
func TestDesignRepairUnsupportedConvertedPublicToken(t *testing.T) {
	t.Run("TestDesignRepairPublicGlobalTokenClassification", TestDesignRepairPublicGlobalTokenClassification)
	t.Run("TestR9H2HostContainerAuthority", TestR9H2HostContainerAuthority)
	t.Run("TestR9H2HostImportAuthority", TestR9H2HostImportAuthority)
}
