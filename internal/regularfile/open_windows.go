//go:build windows

package regularfile

import "os"

func open(path string) (*os.File, error) { return os.Open(path) }
