package shared

// NativeFunctionSizeReport attributes the final native bytes emitted for one
// function. The three top-level regions are exhaustive: a host adapter when one
// is required, optional padding before the register-ABI internal entry, and the
// internal function (including its prologue, body, epilogue, and cold tails).
//
// The reservation fields are subsets of those physical regions. They identify
// bytes that current size-preserving rewrites have made semantically dead and a
// compacting finalizer can remove; they must not be added to TotalBytes.
type NativeFunctionSizeReport struct {
	TotalBytes                    int `json:"total_bytes"`
	HostAdapterBytes              int `json:"host_adapter_bytes"`
	AdapterToInternalPaddingBytes int `json:"adapter_to_internal_padding_bytes"`
	InternalFunctionBytes         int `json:"internal_function_bytes"`

	FrameAdjustmentBytes      int    `json:"frame_adjustment_bytes"`
	DeadFrameReservationBytes int    `json:"dead_frame_reservation_bytes"`
	BranchFoldHoleBytes       int    `json:"branch_fold_hole_bytes"`
	StoreLoadNopBytes         int    `json:"store_load_nop_bytes"`
	LiteralPoolBytes          int    `json:"literal_pool_bytes"`
	HostAdapterShapeHash      uint64 `json:"host_adapter_shape_hash,omitempty"`
	HostAdapterTailBytes      int    `json:"host_adapter_tail_bytes,omitempty"`
	HostAdapterTailShapeHash  uint64 `json:"host_adapter_tail_shape_hash,omitempty"`
}

// DeadReservationBytes returns the exact subset of emitted function bytes that
// existing rewrites have proved semantically dead but cannot yet delete without
// offset remapping.
func (r NativeFunctionSizeReport) DeadReservationBytes() int {
	return r.DeadFrameReservationBytes + r.BranchFoldHoleBytes + r.StoreLoadNopBytes
}

// NativeSizeReport attributes the complete module code image. FunctionBytes
// includes adapters and internal-entry padding because those are owned by each
// function report. FunctionAlignmentBytes is the padding inserted between
// functions. ModuleOtherBytes is reserved for future shared islands or fragments
// that are neither function-owned nor alignment.
type NativeSizeReport struct {
	TotalBytes             int `json:"total_bytes"`
	FunctionBytes          int `json:"function_bytes"`
	FunctionAlignmentBytes int `json:"function_alignment_bytes"`
	ModuleOtherBytes       int `json:"module_other_bytes"`

	HostAdapterBytes              int `json:"host_adapter_bytes"`
	AdapterToInternalPaddingBytes int `json:"adapter_to_internal_padding_bytes"`
	InternalFunctionBytes         int `json:"internal_function_bytes"`
	FrameAdjustmentBytes          int `json:"frame_adjustment_bytes"`
	DeadFrameReservationBytes     int `json:"dead_frame_reservation_bytes"`
	BranchFoldHoleBytes           int `json:"branch_fold_hole_bytes"`
	StoreLoadNopBytes             int `json:"store_load_nop_bytes"`
	LiteralPoolBytes              int `json:"literal_pool_bytes"`
	LiteralPoolUniqueBytes        int `json:"literal_pool_unique_bytes"`
	LiteralPoolDuplicateBytes     int `json:"literal_pool_duplicate_bytes"`
	HostAdapterShapeCount         int `json:"host_adapter_shape_count"`
	HostAdapterCount              int `json:"host_adapter_count"`
	HostAdapterUniqueBytes        int `json:"host_adapter_unique_bytes"`
	HostAdapterDuplicateBytes     int `json:"host_adapter_duplicate_bytes"`
	HostAdapterTailShapeCount     int `json:"host_adapter_tail_shape_count"`
	HostAdapterTailUniqueBytes    int `json:"host_adapter_tail_unique_bytes"`
	HostAdapterTailDuplicateBytes int `json:"host_adapter_tail_duplicate_bytes"`
}

// AdapterShapeHash fingerprints an adapter while replacing its one
// function-specific call relocation with zero bytes. Equal size/hash pairs are
// exact sharing candidates within a module; collection is stats-only.
func AdapterShapeHash(code []byte, relocOff, relocLen int) uint64 {
	const offset64 = 14695981039346656037
	const prime64 = 1099511628211
	h := uint64(offset64)
	for i, b := range code {
		if i >= relocOff && i < relocOff+relocLen {
			b = 0
		}
		h ^= uint64(b)
		h *= prime64
	}
	return h
}

// AccountedBytes returns the exhaustive top-level byte classes. Subset fields
// such as frame adjustments and dead reservations are intentionally excluded.
func (r NativeSizeReport) AccountedBytes() int {
	return r.FunctionBytes + r.FunctionAlignmentBytes + r.ModuleOtherBytes
}

// DeadReservationBytes returns the exact removable subset aggregated across
// all function reports.
func (r NativeSizeReport) DeadReservationBytes() int {
	return r.DeadFrameReservationBytes + r.BranchFoldHoleBytes + r.StoreLoadNopBytes
}

// AddFunction adds one function's exhaustive and subset categories.
func (r *NativeSizeReport) AddFunction(f NativeFunctionSizeReport) {
	if r == nil {
		return
	}
	r.FunctionBytes += f.TotalBytes
	r.HostAdapterBytes += f.HostAdapterBytes
	r.AdapterToInternalPaddingBytes += f.AdapterToInternalPaddingBytes
	r.InternalFunctionBytes += f.InternalFunctionBytes
	r.FrameAdjustmentBytes += f.FrameAdjustmentBytes
	r.DeadFrameReservationBytes += f.DeadFrameReservationBytes
	r.BranchFoldHoleBytes += f.BranchFoldHoleBytes
	r.StoreLoadNopBytes += f.StoreLoadNopBytes
	r.LiteralPoolBytes += f.LiteralPoolBytes
}
