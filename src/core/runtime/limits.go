package runtime

import "fmt"

// InstantiateArenaSize is the fixed arena size used for per-instance runtime
// metadata: host-call log, globals, table descriptor, call buffers, and trap
// buffer. Keep footprint checks in compiler/front-end support code in sync with
// allocations in InstantiateWithImports.
const InstantiateArenaSize = 1 << 20

const HostCallLogBytes = 8 + ((1<<16)/8)*8

// SlotBytes returns the number of bytes needed for n 8-byte Wasm wrapper slots.
// It preserves a non-empty allocation for zero-slot signatures.
func SlotBytes(n int) (int, error) {
	if n <= 0 {
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
	GlobalCount    int
	HasTable       bool
	TableSize      int
	ElemCount      int
	MaxParamSlots  int
	MaxResultSlots int
}

// InstantiateArenaNeed estimates the exact sequence of arena allocations made
// during instance creation, plus a small alignment slack for the allocator's
// 8-byte rounding before each allocation.
func InstantiateArenaNeed(fp InstantiateFootprint) (int, error) {
	if fp.GlobalCount < 0 || fp.TableSize < 0 || fp.ElemCount < 0 || fp.MaxParamSlots < 0 || fp.MaxResultSlots < 0 {
		return 0, fmt.Errorf("negative instantiate footprint input")
	}
	if !fp.HasTable && fp.TableSize != 0 {
		return 0, fmt.Errorf("table size %d without table", fp.TableSize)
	}
	if !fp.HasTable && fp.ElemCount != 0 {
		return 0, fmt.Errorf("element count %d without table", fp.ElemCount)
	}
	if fp.TableSize > (maxInt()-8)/16 {
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
	need := HostCallLogBytes
	if fp.GlobalCount > (maxInt()-need)/16 {
		return 0, fmt.Errorf("global count %d overflows arena allocation", fp.GlobalCount)
	}
	need += 8 * fp.GlobalCount // globals pointer table
	need += 8 * fp.GlobalCount // worst-case cells for local/value-import globals
	if fp.HasTable {
		tableBytes := 8 + fp.TableSize*16
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
