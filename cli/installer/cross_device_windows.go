//go:build windows

package main

import (
	"errors"
	"syscall"
)

// ERROR_NOT_SAME_DEVICE is Windows error code 17.
func isCrossDeviceError(err error) bool { return errors.Is(err, syscall.Errno(17)) }
