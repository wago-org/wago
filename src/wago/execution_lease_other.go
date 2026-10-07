//go:build !amd64 && !arm64

package wago

import "sync"

// Preserve the ordinary lease ABI on targets without private numeric entry.
type executionLease struct{ local *sync.Mutex }

func (l executionLease) privateContext() bool      { return false }
func privateNumericExecutionLease() executionLease { panic("private numeric entry unavailable") }
