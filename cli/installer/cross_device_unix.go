//go:build !windows

package main

import (
	"errors"
	"syscall"
)

func isCrossDeviceError(err error) bool { return errors.Is(err, syscall.EXDEV) }
