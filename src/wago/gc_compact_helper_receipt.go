package wago

// gcCompactHelperReceipt is fresh compiler authority, not public artifact metadata.
// The private execution snapshot retains it; hand-built and loaded metadata do not.
func (c *Compiled) gcCompactHelperReceipt() uint32 {
	if c == nil {
		return 0
	}
	return uint32(c.gcCompactHelperVersion)
}

// gcCompactNativeReceipt attests checked Layout16 native reads only.
func (c *Compiled) gcCompactNativeReceipt() uint32 {
	if c == nil {
		return 0
	}
	return uint32(c.gcCompactNativeVersion)
}
