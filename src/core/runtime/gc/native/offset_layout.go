package gc

import "fmt"

// CompactABIVersion is a helper-only layout receipt, never a native view ABI.
const CompactABIVersion uint32 = 1

type directAllocationWords struct {
	Bump *uint32
	End  uint32
}

// Public StructSize/ArraySize remain the native-header layout. Only collector-
// bound helper constructors may select the separate compact geometry.
func (c *Collector) structObjectSize(d TypeDesc) (uint32, error) {
	if !directHeapCompact || c.direct == nil {
		return StructSize(d)
	}
	if d.Kind != KindStruct {
		return 0, fmt.Errorf("gc: type %d is not a struct", d.ID)
	}
	return compactObjectExtent(d, uint64(d.Size), 4)
}
func (c *Collector) arrayObjectSize(d TypeDesc, n uint32) (uint32, error) {
	if !directHeapCompact || c.direct == nil {
		return ArraySize(d, n)
	}
	if d.Kind != KindArray {
		return 0, fmt.Errorf("gc: type %d is not an array", d.ID)
	}
	return compactObjectExtent(d, uint64(n)*uint64(d.ElemSize), 8)
}
func compactObjectExtent(d TypeDesc, payload uint64, header uint64) (uint32, error) {
	a := uint64(max(d.Align, 1))
	base := (header + a - 1) &^ (a - 1)
	total := base + payload
	if total > uint64(^uint32(0)-15) {
		return 0, ErrAllocationTooLarge
	}
	return uint32(total), nil
}
