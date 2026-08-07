package install

// EnsureRuntime starts installation when no active runtime exists and reports
// whether the caller can continue with the original invocation.
func EnsureRuntime(hasActive func() bool, install func()) bool {
	if hasActive() {
		return true
	}
	install()
	return hasActive()
}
