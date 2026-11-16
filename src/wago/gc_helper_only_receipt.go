package wago

// hasGCHelperOnlyReceipt admits only live compiler output with all native GC
// object lowering disabled. Decoded artifacts have no receipt, even when loaded
// by a helper-only build. This is necessary, not sufficient, for moving-GC
// admission: the adapter must also enforce its supported module/runtime scope.
func (c *Compiled) hasGCHelperOnlyReceipt() bool {
	return c != nil && c.gcHelperOnlyReceipt
}

// gcDirectReadABIReceipt is zero for helper-only, handle-native, and decoded artifacts.
func (c *Compiled) gcDirectReadABIReceipt() uint32 {
	if c == nil {
		return 0
	}
	return c.gcDirectReadABIVersion
}
