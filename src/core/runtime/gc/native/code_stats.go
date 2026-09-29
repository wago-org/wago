package gc

// NativeCodeTelemetry attributes generated native bytes. Producers may report
// zero for unsupported categories, but must keep categories separate rather than
// folding them into TotalBytes.
type NativeCodeTelemetry struct {
	TotalBytes            uint64 `json:"total_bytes"`
	AllocationBytes       uint64 `json:"allocation_bytes"`
	HandleResolutionBytes uint64 `json:"handle_resolution_bytes"`
	TypeCastBytes         uint64 `json:"type_cast_bytes"`
	NullCheckBytes        uint64 `json:"null_check_bytes"`
	BoundsCheckBytes      uint64 `json:"bounds_check_bytes"`
	BarrierBytes          uint64 `json:"barrier_bytes"`
	SpillReloadBytes      uint64 `json:"spill_reload_bytes"`
	HelperCallBytes       uint64 `json:"helper_call_bytes"`
	SharedStubBytes       uint64 `json:"shared_stub_bytes"`
	TrapStubBytes         uint64 `json:"trap_stub_bytes"`
	RootMapBytes          uint64 `json:"root_map_bytes"`
}

// Add merges native-code attribution from multiple modules or architectures.
func (n *NativeCodeTelemetry) Add(other NativeCodeTelemetry) {
	if n == nil {
		return
	}
	n.TotalBytes += other.TotalBytes
	n.AllocationBytes += other.AllocationBytes
	n.HandleResolutionBytes += other.HandleResolutionBytes
	n.TypeCastBytes += other.TypeCastBytes
	n.NullCheckBytes += other.NullCheckBytes
	n.BoundsCheckBytes += other.BoundsCheckBytes
	n.BarrierBytes += other.BarrierBytes
	n.SpillReloadBytes += other.SpillReloadBytes
	n.HelperCallBytes += other.HelperCallBytes
	n.SharedStubBytes += other.SharedStubBytes
	n.TrapStubBytes += other.TrapStubBytes
	n.RootMapBytes += other.RootMapBytes
}
