package runtime

import "fmt"

// InstantiateArenaSize is the maximum supported arena size for per-instance
// runtime metadata: host-call control frame, globals, table descriptor, call
// buffers, and trap buffer. Instantiate maps the exact validated footprint,
// bounded by this limit. Keep footprint checks in compiler/front-end support
// code in sync with allocations in InstantiateWithImports.
const InstantiateArenaSize = 1 << 20

// TableEntryBytes is the size of one indirect-call table descriptor entry:
// {codePtr u64, sigID u32, pad u32, homeLinMem u64, pad u64}. homeLinMem lets
// call_indirect run each funcref in its home instance's context (cross-instance
// linking). The descriptor is [len u32][pad u32][entry...]. The codegen reader
// (backend/railshot callIndirect) and the writer (src/wago instantiate) must use
// this size.
const TableEntryBytes = 32

// SlotBytes returns the number of bytes needed for n 8-byte Wasm wrapper slots,
// preserving a non-empty allocation for zero-slot signatures. A negative count
// indicates a caller bug and is rejected rather than silently treated as zero.
func SlotBytes(n int) (int, error) {
	if n < 0 {
		return 0, fmt.Errorf("negative slot count %d", n)
	}
	if n == 0 {
		return 8, nil
	}
	if n > maxInt()/8 {
		return 0, fmt.Errorf("slot count %d overflows byte count", n)
	}
	return n * 8, nil
}

// InstantiateFootprint describes the per-instance runtime metadata allocations
// made by InstantiateWithImports.
type InstantiateFootprint struct {
	FuncImportCount int
	GlobalCount     int
	HasTable        bool
	TableSize       int
	ElemCount       int
	MaxParamSlots   int
	MaxResultSlots  int
}

// InstantiateArenaNeed estimates the exact sequence of arena allocations made
// during instance creation, plus a small alignment slack for the allocator's
// 8-byte rounding before each allocation.
func InstantiateArenaNeed(fp InstantiateFootprint) (int, error) {
	if fp.FuncImportCount < 0 || fp.GlobalCount < 0 || fp.TableSize < 0 || fp.ElemCount < 0 || fp.MaxParamSlots < 0 || fp.MaxResultSlots < 0 {
		return 0, fmt.Errorf("negative instantiate footprint input")
	}
	if !fp.HasTable && fp.TableSize != 0 {
		return 0, fmt.Errorf("table size %d without table", fp.TableSize)
	}
	if !fp.HasTable && fp.ElemCount != 0 {
		return 0, fmt.Errorf("element count %d without table", fp.ElemCount)
	}
	if fp.TableSize > (maxInt()-8)/TableEntryBytes {
		return 0, fmt.Errorf("table size %d overflows arena allocation", fp.TableSize)
	}
	argsBytes, err := SlotBytes(fp.MaxParamSlots)
	if err != nil {
		return 0, err
	}
	resultsBytes, err := SlotBytes(fp.MaxResultSlots)
	if err != nil {
		return 0, err
	}
	need := 0
	if fp.FuncImportCount > 0 {
		need += HostCtrlFrameBytes
	}
	if fp.GlobalCount > (maxInt()-need)/16 {
		return 0, fmt.Errorf("global count %d overflows arena allocation", fp.GlobalCount)
	}
	need += 8 * fp.GlobalCount // globals pointer table
	need += 8 * fp.GlobalCount // worst-case cells for local/value-import globals
	if fp.HasTable {
		tableBytes := 8 + fp.TableSize*TableEntryBytes
		if need > maxInt()-tableBytes {
			return 0, fmt.Errorf("table size %d overflows arena allocation", fp.TableSize)
		}
		need += tableBytes
	}
	if need > maxInt()-argsBytes || need+argsBytes > maxInt()-resultsBytes || need+argsBytes+resultsBytes > maxInt()-8 {
		return 0, fmt.Errorf("call buffers overflow arena allocation")
	}
	need += argsBytes + resultsBytes + 8 // args, results, trap buffers
	// Arena.Alloc 8-aligns each allocation; reserve a small fixed alignment slack.
	if need > maxInt()-8*8 {
		return 0, fmt.Errorf("instantiate footprint overflows arena allocation")
	}
	need += 8 * 8
	return need, nil
}

func maxInt() int { return int(^uint(0) >> 1) }
