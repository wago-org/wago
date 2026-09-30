//go:build windows

package profile

import "os"

func openInput(path string) (*os.File, error) { return os.Open(path) }
