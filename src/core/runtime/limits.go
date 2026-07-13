package runtime

import "fmt"

// InstantiateArenaSize is the maximum supported arena size for per-instance
// runtime metadata: host-call log, globals, table descriptor, call buffers, and
// trap buffer. Instantiate maps the exact validated footprint, bounded by this
// limit. Keep footprint checks in compiler/front-end support code in sync with
// allocations in InstantiateWithImports.
const InstantiateArenaSize = 1 << 20

const HostCallLogBytes = 8 + ((1<<16)/8)*8

// PassiveElemDescBytes is the size of one passive element segment descriptor:
// {ptr u64, len u32, pad u32}. elem.drop zeroes len. The ptr targets an array
// of TableEntryBytes descriptors so table.init can copy directly into table 0.
const PassiveElemDescBytes = 16

// TableEntryBytes is the size of one indirect-call table descriptor entry:
// {codePtr u64, sigID u32, pad u32, homeLinMem u64, refSlot u64}. homeLinMem lets
// call_indirect run each funcref in its home instance's context (cross-instance
// linking), and refSlot carries the nullable descriptor-pointer funcref handle
// returned by table.get.
// The descriptor is [len u32][max u32][entry...]. The codegen reader
// (backend/railshot callIndirect/table.*) and the writer (src/wago instantiate)
// must use this size and these offsets.
const (
	TableEntryCodePtrOffset    = 0
	TableEntrySigIDOffset      = 8
	TableEntryHomeLinMemOffset = 16
	TableEntryRefSlotOffset    = 24
	TableEntryBytes            = 32
)

// PassiveDataDescBytes is the size of one passive data segment descriptor:
// {ptr u64, len u32, pad u32}. The JIT reads these for memory.init/data.drop.
const PassiveDataDescBytes = 16

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
	FuncImportCount    int
	ImportBindingBytes int // per-instance dynamic cross-import descriptor storage
	HostCallBytes      int // explicit sync control-frame bytes; zero selects the legacy async log for function imports
	FuncRefCount       int
	GlobalCount        int
	HasTable           bool
	TableSize          int
	TableCapacity      int
	TableCapacities    []int // when non-empty, one capacity per table index; imported entries are skipped
	TableEntryBytes    []int // when non-empty, one type-specific entry stride per table; legacy nil means funcref
	ImportedTableCount int   // leading table indexes whose descriptors are externally owned
	ElemCount          int
	PassiveElemCount   int
	PassiveElemBytes   int // type-specific per-instance payload bytes for passive segments
	PassiveDataCount   int
	MaxParamSlots      int
	MaxResultSlots     int
}

// InstantiateArenaNeed estimates the exact sequence of arena allocations made
// during instance creation, plus a small alignment slack for the allocator's
// 8-byte rounding before each allocation.
func InstantiateArenaNeed(fp InstantiateFootprint) (int, error) {
	if fp.FuncImportCount < 0 || fp.ImportBindingBytes < 0 || fp.HostCallBytes < 0 || fp.FuncRefCount < 0 || fp.GlobalCount < 0 || fp.TableSize < 0 || fp.TableCapacity < 0 || fp.ImportedTableCount < 0 || fp.ElemCount < 0 || fp.PassiveElemCount < 0 || fp.PassiveElemBytes < 0 || fp.PassiveDataCount < 0 || fp.MaxParamSlots < 0 || fp.MaxResultSlots < 0 {
		return 0, fmt.Errorf("negative instantiate footprint input")
	}
	tableCaps := fp.TableCapacities
	if len(tableCaps) == 0 {
		if !fp.HasTable && fp.TableSize != 0 {
			return 0, fmt.Errorf("table size %d without table", fp.TableSize)
		}
		if fp.TableCapacity == 0 {
			fp.TableCapacity = fp.TableSize
		}
		if !fp.HasTable && fp.TableCapacity != 0 {
			return 0, fmt.Errorf("table capacity %d without table", fp.TableCapacity)
		}
		if fp.TableCapacity < fp.TableSize {
			return 0, fmt.Errorf("table capacity %d < size %d", fp.TableCapacity, fp.TableSize)
		}
		if fp.HasTable {
			tableCaps = []int{fp.TableCapacity}
		}
	} else {
		if !fp.HasTable {
			return 0, fmt.Errorf("table capacities without table")
		}
		if fp.TableSize != 0 || fp.TableCapacity != 0 {
			return 0, fmt.Errorf("legacy table footprint mixed with table capacities")
		}
	}
	if fp.ImportedTableCount > len(tableCaps) {
		return 0, fmt.Errorf("imported table count %d exceeds table count %d", fp.ImportedTableCount, len(tableCaps))
	}
	tableEntryBytes := fp.TableEntryBytes
	if len(tableEntryBytes) != 0 && len(tableEntryBytes) != len(tableCaps) {
		return 0, fmt.Errorf("table entry stride count %d != table count %d", len(tableEntryBytes), len(tableCaps))
	}
	entryStride := func(index int) int {
		if len(tableEntryBytes) == 0 {
			return TableEntryBytes
		}
		return tableEntryBytes[index]
	}
	for i, capacity := range tableCaps {
		if capacity < 0 {
			return 0, fmt.Errorf("negative table %d capacity %d", i, capacity)
		}
		stride := entryStride(i)
		if stride != 8 && stride != TableEntryBytes {
			return 0, fmt.Errorf("table %d entry stride %d is unsupported", i, stride)
		}
		if capacity > (maxInt()-8)/stride {
			return 0, fmt.Errorf("table %d capacity %d with stride %d overflows arena allocation", i, capacity, stride)
		}
	}
	if fp.FuncRefCount > maxInt()/TableEntryBytes {
		return 0, fmt.Errorf("funcref descriptor count %d overflows arena allocation", fp.FuncRefCount)
	}
	argsBytes, err := SlotBytes(fp.MaxParamSlots)
	if err != nil {
		return 0, err
	}
	resultsBytes, err := SlotBytes(fp.MaxResultSlots)
	if err != nil {
		return 0, err
	}
	need := fp.HostCallBytes
	if need > maxInt()-fp.ImportBindingBytes {
		return 0, fmt.Errorf("import binding bytes overflow arena allocation")
	}
	need += fp.ImportBindingBytes
	if need == 0 && fp.FuncImportCount > 0 {
		need += HostCallLogBytes
	}
	if fp.GlobalCount > (maxInt()-need)/16 {
		return 0, fmt.Errorf("global count %d overflows arena allocation", fp.GlobalCount)
	}
	need += 8 * fp.GlobalCount // globals pointer table
	need += 8 * fp.GlobalCount // worst-case cells for local/value-import globals
	if len(tableCaps) > 1 {
		if len(tableCaps) > (maxInt()-need)/8 {
			return 0, fmt.Errorf("table count %d overflows directory allocation", len(tableCaps))
		}
		need += 8 * len(tableCaps)
	}
	for i, capacity := range tableCaps {
		if i < fp.ImportedTableCount {
			continue
		}
		tableBytes := 8 + capacity*entryStride(i)
		if need > maxInt()-tableBytes {
			return 0, fmt.Errorf("table %d capacity %d overflows arena allocation", i, capacity)
		}
		need += tableBytes
	}
	funcRefBytes := fp.FuncRefCount * TableEntryBytes
	if need > maxInt()-funcRefBytes {
		return 0, fmt.Errorf("funcref descriptor count %d overflows arena allocation", fp.FuncRefCount)
	}
	need += funcRefBytes
	passiveElemBytes := fp.PassiveElemCount * PassiveElemDescBytes
	if need > maxInt()-passiveElemBytes {
		return 0, fmt.Errorf("passive element count %d overflows arena allocation", fp.PassiveElemCount)
	}
	need += passiveElemBytes
	if need > maxInt()-fp.PassiveElemBytes {
		return 0, fmt.Errorf("passive element payload bytes %d overflow arena allocation", fp.PassiveElemBytes)
	}
	need += fp.PassiveElemBytes
	if fp.PassiveDataCount > (maxInt()-need)/PassiveDataDescBytes {
		return 0, fmt.Errorf("passive data count %d overflows arena allocation", fp.PassiveDataCount)
	}
	need += fp.PassiveDataCount * PassiveDataDescBytes
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
