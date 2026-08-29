package runtime

import "fmt"

type resourceLimitSentinel string

func (e resourceLimitSentinel) Error() string { return string(e) }

// ErrResourceLimit identifies a runtime resource limit. Use errors.As with
// ResourceLimitError to get the resource name and the limit values.
const ErrResourceLimit = resourceLimitSentinel("wago: resource limit exceeded")

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
