//go:build amd64

package wago

import "sync"

type executionLease struct {
	local *sync.Mutex
	// private denotes an engine-owned numeric context; no native resource lease.
	private bool
}

func (l executionLease) privateContext() bool      { return l.private }
func privateNumericExecutionLease() executionLease { return executionLease{private: true} }
