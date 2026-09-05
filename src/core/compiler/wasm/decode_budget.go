package wasm

import (
	"fmt"
	"unsafe"
)

// DecodeLimits bounds type count and aggregate metadata reservations before
// allocation. Reservations include vector growth and temporary decode state.
// Zero fields select the defaults. Input body/data bytes remain borrowed.
type DecodeLimits struct {
	MaxTypes         uint64
	MaxMetadataBytes uint64
}

func DefaultDecodeLimits() DecodeLimits {
	return DecodeLimits{MaxTypes: 100000, MaxMetadataBytes: 256 << 20}
}

type decodeBudget struct {
	remaining uint64
	types     uint64
	limits    DecodeLimits
}

func newDecodeBudget(limits DecodeLimits) *decodeBudget {
	defaults := DefaultDecodeLimits()
	if limits.MaxTypes == 0 {
		limits.MaxTypes = defaults.MaxTypes
	}
	if limits.MaxMetadataBytes == 0 {
		limits.MaxMetadataBytes = defaults.MaxMetadataBytes
	}
	return &decodeBudget{remaining: limits.MaxMetadataBytes, limits: limits}
}
func (r *reader) reserve(count, width uint64) error {
	if r.budget == nil {
		r.budget = newDecodeBudget(DecodeLimits{})
	}
	if width != 0 && count > r.budget.remaining/width {
		return fmt.Errorf("wasm decode metadata exceeds allocation limit %d at offset %d", r.budget.limits.MaxMetadataBytes, r.off())
	}
	r.budget.remaining -= count * width
	return nil
}
func reserveDecodedSlice[T any](r *reader, n uint32) error {
	var value T
	// Include old growth buffers, pointer sidecars and allocator rounding.
	return r.reserve(uint64(n), uint64(unsafe.Sizeof(value))*8+16)
}

// reserveMetadataStorage covers one exact-capacity allocation, including
// allocator size-class rounding. Unlike growing AST vectors, it needs no
// allowance for old growth buffers.
func reserveMetadataStorage[T any](r *reader, n uint32) error {
	var value T
	if n == 0 {
		return nil
	}
	if err := r.reserve(uint64(n), uint64(unsafe.Sizeof(value))); err != nil {
		return err
	}
	// Small allocations round to size classes; large ones round to pages.
	return r.reserve(1, min(uint64(n)*uint64(unsafe.Sizeof(value)), 8192))
}

// All structured metadata entries consume at least one encoded byte. Allocate
// their complete checked vector once instead of retaining growth buffers.
func readMetadataVec[T any](r *reader, fn func(*reader) (T, error)) ([]T, error) {
	n, err := r.u32()
	if err != nil {
		return nil, err
	}
	if uint64(n) > uint64(r.left()) {
		return nil, &DecodeError{Code: ErrIndexOutOfBounds, Offset: r.off()}
	}
	if err := reserveMetadataStorage[T](r, n); err != nil {
		return nil, err
	}
	out := make([]T, n)
	for i := range out {
		out[i], err = fn(r)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (r *reader) reserveType() error {
	if r.budget == nil {
		r.budget = newDecodeBudget(DecodeLimits{})
	}
	if r.budget.types >= r.budget.limits.MaxTypes {
		return fmt.Errorf("wasm decode type count exceeds limit %d", r.budget.limits.MaxTypes)
	}
	r.budget.types++
	return nil
}

// decodeTypeSection keeps implicit singleton groups in one shared backing slab.
// Explicit recursive groups retain their own contiguous subtype vectors.
func decodeTypeSection(r *reader) ([]RecType, error) {
	n, err := r.u32()
	if err != nil {
		return nil, err
	}
	if r.budget == nil {
		r.budget = newDecodeBudget(DecodeLimits{})
	}
	if uint64(n) > r.budget.limits.MaxTypes {
		return nil, fmt.Errorf("wasm decode recursive group count exceeds limit %d", r.budget.limits.MaxTypes)
	}
	if uint64(n) > uint64(r.left()) {
		return nil, &DecodeError{Code: ErrIndexOutOfBounds, Offset: r.off()}
	}
	if err := reserveDecodedSlice[RecType](r, n); err != nil {
		return nil, err
	}
	if err := reserveDecodedSlice[SubType](r, n); err != nil {
		return nil, err
	}
	groups := make([]RecType, n)
	singletons := make([]SubType, n)
	used := 0
	for i := range groups {
		if b, ok := r.peek(); ok && b == 0x4e {
			groups[i], err = decodeRecType(r)
		} else {
			singletons[used], err = decodeSubType(r)
			groups[i].SubTypes = singletons[used : used+1 : used+1]
			used++
		}
		if err != nil {
			return nil, err
		}
	}
	return groups, nil
}
