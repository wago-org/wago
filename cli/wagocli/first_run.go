package wagocli

func ensureFirstRunRuntime(hasActive func() bool, install func()) bool {
	if hasActive() {
		return true
	}
	install()
	return hasActive()
}
