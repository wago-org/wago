package wago

func cleanupTestPanic(fn func()) (value any, panicked bool) {
	returned := false
	defer func() {
		if !returned {
			value = recover()
			panicked = true
		}
	}()
	fn()
	returned = true
	return nil, false
}
