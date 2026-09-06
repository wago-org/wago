//go:build windows

package version

// atomicfile publishes with MOVEFILE_WRITE_THROUGH on Windows.
func syncActiveStateDirectory(string) error { return nil }
