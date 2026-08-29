package wago

import coreruntime "github.com/wago-org/wago/src/core/runtime"

// ErrResourceLimit identifies a configured or fixed runtime resource limit.
// Use errors.As with ResourceLimitError to inspect the rejected request.
const ErrResourceLimit = coreruntime.ErrResourceLimit

// ResourceLimitError reports one rejected resource request.
type ResourceLimitError = coreruntime.ResourceLimitError

// NativeMemoryStats reports process use of the Linux host interrupt memory
// registry. The type contains zero values on unsupported targets.
type NativeMemoryStats = coreruntime.NativeMemoryStats

// ProcessNativeMemoryStats returns a lock-free process snapshot.
func ProcessNativeMemoryStats() NativeMemoryStats {
	return coreruntime.ProcessNativeMemoryStats()
}

// RuntimeResourceStats reports configured admission counters for a Runtime.
// Direct instance counters are active when WithInstanceLimits configures at
// least one direct-instance limit. Native memory counters are active when
// WithNativeMemoryMappingLimit configures a nonzero limit.
type RuntimeResourceStats struct {
	DirectInstances                  uint32
	DirectDeclaredMemoryBytes        uint64
	NativeMemoryMappings             uint32
	PeakNativeMemoryMappings         uint32
	MaxDirectInstances               uint32
	MaxDirectDeclaredMemoryBytes     uint64
	MaxNativeMemoryMappings          uint32
	DirectInstancesTracked           bool
	DirectDeclaredMemoryBytesTracked bool
	NativeMemoryMappingsTracked      bool
}

// ResourceStats returns a consistent snapshot of this Runtime's configured
// resource counters.
func (rt *Runtime) ResourceStats() RuntimeResourceStats {
	if rt == nil {
		return RuntimeResourceStats{}
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	stats := RuntimeResourceStats{
		DirectInstances:           rt.directInstanceCount,
		DirectDeclaredMemoryBytes: rt.directInstanceMemory,
		NativeMemoryMappings:      rt.nativeMemoryMappings,
		PeakNativeMemoryMappings:  rt.nativeMemoryMappingsPeak,
	}
	if rt.cfg == nil || rt.cfg.instanceLimits == nil {
		return stats
	}
	limits := rt.cfg.instanceLimits
	stats.MaxDirectInstances = limits.maxInstances
	stats.MaxDirectDeclaredMemoryBytes = limits.maxMemoryBytes
	stats.MaxNativeMemoryMappings = limits.maxNativeMemoryMappings
	stats.DirectInstancesTracked = limits.maxInstances != 0 || limits.maxMemoryBytes != 0
	stats.DirectDeclaredMemoryBytesTracked = limits.maxMemoryBytes != 0
	stats.NativeMemoryMappingsTracked = limits.maxNativeMemoryMappings != 0
	return stats
}
