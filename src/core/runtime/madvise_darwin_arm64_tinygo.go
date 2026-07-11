//go:build darwin && arm64 && tinygo

package runtime

// TinyGo's Darwin syscall surface does not expose madvise. Clearing preserves
// the zero-on-reuse contract; it merely forgoes returning those pages to the OS.
func madviseDontNeed(b []byte) error {
	clear(b)
	return nil
}
