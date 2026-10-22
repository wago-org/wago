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

// releaseRetainedResourceRoots drains an already-claimed slice of exact root
// units outside container locks. Separate entries may own independent roots on
// the same Instance, so this helper must not deduplicate by owner. Clearing a
// slot before the actual decrement permits reentrant local drains to visit only
// still-owned units. Producer callbacks may panic after the unit is consumed.
func releaseRetainedResourceRoots(roots []*Instance) {
	var cleanup resourceCleanup
	for i := range roots {
		owner := roots[i] // Reread after any recursive drain of this same batch.
		roots[i] = nil
		if owner != nil {
			cleanup.run(owner.releaseResourceRoot)
		}
	}
	cleanup.finish()
}
