package wago

// resourceCleanup delays an ordinary cleanup panic until all remaining owned
// units in the enclosing boundary have been visited. It preserves the first
// panic value; it does not convert failure into successful physical teardown.
// Callers must retain any concrete handle whose release has not consumed it.
// Goexit is not a recoverable panic and cannot be contained by this boundary.
type resourceCleanup struct {
	panicked bool
	value    any
}

func (c *resourceCleanup) run(fn func()) {
	returned := false
	defer func() {
		if !returned {
			value := recover()
			if !c.panicked {
				c.panicked = true
				c.value = value
			}
		}
	}()
	fn()
	returned = true
}

func (c *resourceCleanup) finish() {
	if c.panicked {
		panic(c.value)
	}
}
