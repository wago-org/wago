//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

type swarElement struct {
	kind      wasm.ElemModeKind
	offset    uint32
	functions []uint32
}

// Keep original indices and count all declaration payloads, including dropped
// active/declarative segments. The module pipeline validates the whole shape
// with admitOwnedTable before this helper checks and copies each segment.
func swarModuleElements(elements []wasm.Elem) ([]swarElement, error) {
	if len(elements) > MaxOwnedElementSegments {
		return nil, unsupported("SWAR element count")
	}
	remaining := MaxOwnedTableEntries
	for _, e := range elements {
		if len(e.Kind.Funcs) > remaining {
			return nil, unsupported("SWAR element aggregate bound")
		}
		remaining -= len(e.Kind.Funcs)
	}
	owned := make([]swarElement, len(elements))
	for i, e := range elements {
		if e.Kind.Kind != wasm.ElemFuncs || len(e.Kind.Exprs) != 0 || e.Mode.Table != 0 {
			return nil, unsupported("SWAR legacy owned element shape")
		}
		switch e.Mode.Kind {
		case wasm.ElemActive:
			offset, err := plainConst(e.Mode.Offset, false)
			if err != nil {
				return nil, err
			}
			owned[i].offset = uint32(offset)
		case wasm.ElemPassive, wasm.ElemDeclarative:
			if len(e.Mode.Offset.BodyBytes) != 0 || len(e.Mode.Offset.Instrs) != 0 {
				return nil, unsupported("SWAR inactive element offset")
			}
		default:
			return nil, unsupported("SWAR element mode")
		}
		owned[i].kind = e.Mode.Kind
		owned[i].functions = make([]uint32, len(e.Kind.Funcs))
		for n, index := range e.Kind.Funcs {
			owned[i].functions[n] = uint32(index)
		}
	}
	return owned, nil
}

// A private owner requires exclusive idle JobMemory that stays live through
// owner.Close, no saved/queued contexts,
// and complete invocation joins before Close. The caller owns code and must
// keep it unchanged/live until every table owner using it has closed. Seal is
// idempotent and guarantees W^X/process-wide cache synchronization before any
// callable descriptor is bound. This does not expose a public execution API.
type swarTableOwner struct {
	arena                      *coreruntime.Arena
	job                        *coreruntime.JobMemory
	code                       *coreruntime.CodeBuffer
	table, functions, elements uintptr
}

func (image *swarModuleImage) instantiateTables(job *coreruntime.JobMemory, code *coreruntime.CodeBuffer) (*swarTableOwner, error) {
	return image.instantiateTablesWithArena(job, code, coreruntime.NewArena)
}

func (image *swarModuleImage) instantiateTablesWithArena(job *coreruntime.JobMemory, code *coreruntime.CodeBuffer, allocate func(int) (*coreruntime.Arena, error)) (*swarTableOwner, error) {
	if image == nil || job == nil {
		return nil, unsupported("SWAR table owner image/job")
	}
	if image.table == nil {
		if len(image.elements) != 0 || len(image.functionKeys) != 0 {
			return nil, unsupported("SWAR table owner missing table")
		}
		return &swarTableOwner{}, nil
	}
	if err := swarOwnedTableType(image.table); err != nil {
		return nil, err
	}
	ctx := job.CaptureInstanceContext()
	if ctx.TablePtr != 0 || ctx.FuncRefDescPtr != 0 || ctx.PassiveElemPtr != 0 {
		return nil, unsupported("SWAR table owner requires unbound job")
	}
	if allocate == nil || code == nil || len(image.code) == 0 || len(image.code) > MaxCodeBytes || len(code.Bytes()) != len(image.code) || !bytes.Equal(code.Bytes(), image.code) || len(image.functions) == 0 || len(image.functions) > MaxFunctions || len(image.functionKeys) != len(image.functions) || len(image.elements) > MaxOwnedElementSegments {
		return nil, unsupported("SWAR table owner image/code/directory bound")
	}
	previousEnd := 0
	for i, info := range image.functions {
		if info.Entry < previousEnd || info.Entry&15 != 0 || info.CodeBytes <= 0 || info.CodeBytes&3 != 0 || info.Entry > len(image.code) || info.CodeBytes > len(image.code)-info.Entry || info.FrameHeaderBytes != callFrameHeader || info.FrameBytes < callFrameHeader || info.FrameBytes > MaxFrameBytes || info.FrameBytes&15 != 0 || !info.mode.production || info.mode.functionIndex != uint32(i) || info.Params < 0 || info.Params > 256 || info.Results < 0 || info.Results > 256 {
			return nil, unsupported("SWAR table owner framed entries")
		}
		previousEnd = info.Entry + info.CodeBytes
	}
	capacity := int(image.table.Limits.Min)
	if image.table.Limits.HasMax {
		capacity = int(image.table.Limits.Max)
	} else if capacity <= MaxOwnedTableEntries/2 {
		capacity = MaxOwnedTableEntries
	}
	payloadEntries := 0
	for i, e := range image.elements {
		if len(e.functions) > MaxOwnedTableEntries-payloadEntries {
			return nil, unsupported("SWAR table owner element payload bound")
		}
		payloadEntries += len(e.functions)
		if e.kind != wasm.ElemActive && e.kind != wasm.ElemPassive && e.kind != wasm.ElemDeclarative {
			return nil, unsupported("SWAR table owner element mode")
		}
		if e.kind != wasm.ElemActive && e.offset != 0 {
			return nil, unsupported("SWAR table owner inactive offset")
		}
		if e.kind == wasm.ElemActive && (uint64(e.offset) > image.table.Limits.Min || uint64(len(e.functions)) > image.table.Limits.Min-uint64(e.offset)) {
			return nil, fmt.Errorf("rv64: active element segment %d out of bounds", i)
		}
		for _, index := range e.functions {
			if uint64(index) >= uint64(len(image.functions)) {
				return nil, unsupported("SWAR table owner local function index")
			}
		}
	}
	// All shape/range checks precede sealing or allocation. A seal failure stays
	// attached to the caller's CodeBuffer; no instance pointer was published.
	if err := code.Seal(); err != nil {
		return nil, err
	}
	base := code.Base()
	if base == 0 || base&15 != 0 {
		return nil, unsupported("SWAR table owner code alignment")
	}
	tableBytes, functionBytes, elementBytes := 8+capacity*32, (len(image.functions)+1)*40, len(image.elements)*16
	arena, err := allocate(tableBytes + functionBytes + elementBytes + payloadEntries*32 + 8)
	if err != nil {
		return nil, err
	}
	table, functions, elements, context := arena.Alloc(tableBytes), arena.Alloc(functionBytes), arena.Alloc(elementBytes), arena.Alloc(8)
	clear(table)
	clear(functions)
	clear(elements)
	clear(context)
	binary.LittleEndian.PutUint32(table, uint32(image.table.Limits.Min))
	binary.LittleEndian.PutUint32(table[4:], uint32(capacity))
	contextBits := uint64(uintptr(unsafe.Pointer(&context[0])))
	binary.LittleEndian.PutUint64(functions[32:], contextBits)
	for i, info := range image.functions {
		desc := functions[(i+1)*40 : (i+2)*40]
		binary.LittleEndian.PutUint64(desc, uint64(base)+uint64(info.Entry))
		binary.LittleEndian.PutUint64(desc[8:], image.functionKeys[i])
		binary.LittleEndian.PutUint64(desc[16:], uint64(job.LinMemBase())|uint64(abi.FuncRefLocalWrapperHomeTag))
		binary.LittleEndian.PutUint64(desc[24:], uint64(uintptr(unsafe.Pointer(&desc[0]))))
		binary.LittleEndian.PutUint64(desc[32:], contextBits)
	}
	for i, e := range image.elements {
		if e.kind != wasm.ElemPassive || len(e.functions) == 0 {
			continue
		}
		payload := arena.Alloc(len(e.functions) * 32)
		for n, index := range e.functions {
			copy(payload[n*32:(n+1)*32], functions[(int(index)+1)*40:(int(index)+1)*40+32])
		}
		binary.LittleEndian.PutUint64(elements[i*16:], uint64(uintptr(unsafe.Pointer(&payload[0]))))
		binary.LittleEndian.PutUint32(elements[i*16+8:], uint32(len(e.functions)))
	}
	// Validated active segments apply in source order; all were range-checked.
	for _, e := range image.elements {
		if e.kind == wasm.ElemActive {
			for n, index := range e.functions {
				offset := 8 + (int(e.offset)+n)*32
				copy(table[offset:offset+32], functions[(int(index)+1)*40:(int(index)+1)*40+32])
			}
		}
	}
	owner := &swarTableOwner{arena: arena, job: job, code: code, table: uintptr(unsafe.Pointer(&table[0])), functions: uintptr(unsafe.Pointer(&functions[0]))}
	if len(elements) != 0 {
		owner.elements = uintptr(unsafe.Pointer(&elements[0]))
	}
	job.SetFuncRefDesc(owner.functions)
	job.SetPassiveElemPtr(owner.elements)
	job.SetTablePtr(owner.table)
	return owner, nil
}

func (owner *swarTableOwner) Close() error { return owner.closeWith((*coreruntime.Arena).Close) }
func (owner *swarTableOwner) closeWith(release func(*coreruntime.Arena) error) error {
	if owner == nil || owner.arena == nil {
		return nil
	}
	ctx := owner.job.CaptureInstanceContext()
	if ctx.TablePtr != 0 && ctx.TablePtr != owner.table || ctx.FuncRefDescPtr != 0 && ctx.FuncRefDescPtr != owner.functions || ctx.PassiveElemPtr != 0 && ctx.PassiveElemPtr != owner.elements {
		return unsupported("SWAR table owner binding replaced")
	}
	owner.job.SetTablePtr(0)
	owner.job.SetFuncRefDesc(0)
	owner.job.SetPassiveElemPtr(0)
	if err := release(owner.arena); err != nil {
		return err
	}
	owner.arena, owner.job, owner.code = nil, nil, nil
	owner.table, owner.functions, owner.elements = 0, 0, 0
	return nil
}
