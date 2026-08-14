//go:build windows

package project

// atomicfile uses MoveFileEx with MOVEFILE_WRITE_THROUGH on Windows. Opening
// directories for FlushFileBuffers is not portable through os.File.
func syncProjectDirectory(string) error { return nil }
