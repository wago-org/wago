package runtime

import "fmt"

type limitClassSentinel string

func (e limitClassSentinel) Error() string { return string(e) }

const (
	// ErrResourceLimit identifies a real finite resource or configured quota.
	ErrResourceLimit = limitClassSentinel("wago: resource limit exceeded")
	// ErrUnsupported identifies valid WebAssembly whose feature or shape Wago
	// does not implement.
	ErrUnsupported = limitClassSentinel("wago: unsupported feature or shape")
	// ErrImplementationLimit identifies a temporary internal representation
	// limit. It is not a host security policy or resource quota.
	ErrImplementationLimit = limitClassSentinel("wago: implementation limit exceeded")
)

// ImplementationLimitError reports a valid operation that cannot be represented
// by the current implementation. Resource telemetry must not count this error.
type ImplementationLimitError struct {
	Feature string
	Shape   string
	Limit   uint64
	Cause   error
}

func (e *ImplementationLimitError) Error() string {
	if e == nil {
		return ErrImplementationLimit.Error()
	}
	feature := e.Feature
	if feature == "" {
		feature = "operation"
	}
	message := fmt.Sprintf("wago: %s exceeds an implementation limit", feature)
	if e.Shape != "" {
		message += ": " + e.Shape
	}
	if e.Limit != 0 {
		message += fmt.Sprintf(" (limit %d)", e.Limit)
	}
	return message
}

func (e *ImplementationLimitError) Unwrap() []error {
	if e != nil && e.Cause != nil {
		return []error{ErrImplementationLimit, e.Cause}
	}
	return []error{ErrImplementationLimit}
}

// ResourceLimitError reports one resource request that a configured or fixed
// limit rejected.
type ResourceLimitError struct {
	Resource   string
	Scope      string
	Used       uint64
	Requested  uint64
	Limit      uint64
	Suggestion string
	Cause      error
}

func (e *ResourceLimitError) Error() string {
	if e == nil {
		return ErrResourceLimit.Error()
	}
	resource := e.Resource
	if resource == "" {
		resource = "resource"
	}
	scope := e.Scope
	if scope != "" {
		scope += " "
	}
	message := fmt.Sprintf("wago: %s%s limit exceeded: used %d, requested %d, limit %d", scope, resource, e.Used, e.Requested, e.Limit)
	if e.Suggestion != "" {
		message += "; " + e.Suggestion
	}
	return message
}

// Unwrap makes every ResourceLimitError match ErrResourceLimit. Cause is an
// optional second classification, such as a public permission error.
func (e *ResourceLimitError) Unwrap() []error {
	if e != nil && e.Cause != nil {
		return []error{ErrResourceLimit, e.Cause}
	}
	return []error{ErrResourceLimit}
}

// NativeMemoryStats is a process snapshot of the mappings that the Linux host
// interrupt handler can authenticate. Supported is false on other targets.
// Registered includes active and cached mappings. ScanSpan is the number of
// registry entries that a signal handler can inspect.
type NativeMemoryStats struct {
	Supported      bool
	Active         uint32
	Cached         uint32
	Registered     uint32
	PeakRegistered uint32
	Capacity       uint32
	ScanSpan       uint32
}

var nativeMemoryStatsSnapshot func() NativeMemoryStats
var interruptLinearMemoryCacheChange func(int32)

// ProcessNativeMemoryStats returns a lock-free process snapshot. Values can
// change while the function reads them. The function returns zero values when
// the build does not use the Linux host interrupt registry.
func ProcessNativeMemoryStats() NativeMemoryStats {
	if nativeMemoryStatsSnapshot == nil {
		return NativeMemoryStats{}
	}
	return nativeMemoryStatsSnapshot()
}

func changeInterruptLinearMemoryCache(delta int32) {
	if interruptLinearMemoryCacheChange != nil {
		interruptLinearMemoryCacheChange(delta)
	}
}
