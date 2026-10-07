//go:build darwin

package build

func retainBuildReplaceHandle(bool) bool {
	// Darwin ACL grants can override mode 0600, so every artifact stage remains in
	// the private directory. Missing-output inheritance is sampled separately by
	// an empty probe and applied only after the complete bytes have been written.
	return true
}
