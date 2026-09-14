// Package runtimebridge contains capability markers for narrow calls between
// Wago's public runtime layer and its lower-level native runtime package.
package runtimebridge

// HostScalarCallAccess gates construction of cached native host-call state.
// Its zero value is invalid, including when manufactured through reflection.
type HostScalarCallAccess struct{ granted bool }

// GrantHostScalarCall returns the capability used by Wago's instance layer.
// Go's internal-package rule prevents external modules from calling it.
func GrantHostScalarCall() HostScalarCallAccess { return HostScalarCallAccess{granted: true} }

// Granted reports whether this marker came from GrantHostScalarCall.
func (a HostScalarCallAccess) Granted() bool { return a.granted }
