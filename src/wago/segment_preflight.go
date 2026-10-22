package wago

import (
	"encoding/binary"
	"fmt"
	"unsafe"

	"github.com/wago-org/wago/src/core/runtime"
)

// Core 1 validates every active segment's bounds before modifying shared store
// state. Core 2's bulk-memory semantics instead retain earlier completed writes.
func core1SegmentOffset(c *Compiled, globals []*Global, offset OffsetInit, context constExprContext, addr64 bool) (uint64, error) {
	value := uint64(offset.Base)
	if offset.HasGlobal {
		if offset.Global < 0 || offset.Global >= len(c.Globals) || offset.Global >= len(globals) || globals[offset.Global] == nil {
			return 0, fmt.Errorf("active segment offset global %d out of range", offset.Global)
		}
		value = readGlobalObject(globals[offset.Global], c.Globals[offset.Global].Type)
		if !addr64 {
			value = uint64(uint32(value))
		}
	}
	if len(offset.Expr) != 0 {
		want := ValI32
		if addr64 {
			want = ValI64
		}
		bits, err := evalCompiledScalarConstExpr(offset.Expr, want, globals, c.Globals, constExprGlobalScope{context: context, limit: len(c.Globals)})
		if err != nil {
			return 0, err
		}
		value = bits
		if !addr64 {
			value = uint64(uint32(value))
		}
	}
	return value, nil
}

func preflightCore1DataSegments(c *Compiled, globals []*Global, primary *runtime.JobMemory, memories []*Memory) error {
	for seg := 0; seg < c.activeDataCount(); seg++ {
		data := c.activeDataAt(seg)
		memory := primary
		if data.MemoryIndex != 0 {
			if int(data.MemoryIndex) >= len(memories) || memories[data.MemoryIndex] == nil {
				return fmt.Errorf("active data segment %d memory index %d is unavailable", seg, data.MemoryIndex)
			}
			memory = memories[data.MemoryIndex].jobMemory()
		}
		bytes, err := memory.HostBytesChecked()
		if err != nil {
			return fmt.Errorf("active data segment %d host access: %w", seg, err)
		}
		offset, err := core1SegmentOffset(c, globals, data.Offset, constExprDataOffset, c.memoryDef(int(data.MemoryIndex)).Addr64)
		if err != nil {
			return fmt.Errorf("active data segment %d offset: %w", seg, err)
		}
		if offset > uint64(len(bytes)) || uint64(len(data.Bytes)) > uint64(len(bytes))-offset {
			return fmt.Errorf("active data segment %d out of bounds on memory %d: offset %d + length %d > memory size %d", seg, data.MemoryIndex, offset, len(data.Bytes), len(bytes))
		}
	}
	return nil
}

func preflightCore1ElementSegments(c *Compiled, globals []*Global, primary, directory []byte) error {
	for seg, element := range c.Elems {
		header := primary
		if element.TableIndex != 0 {
			ptr := uintptr(binary.LittleEndian.Uint64(directory[int(element.TableIndex)*8:]))
			header = unsafe.Slice((*byte)(offHeapPtr(ptr)), 8)
		}
		size := uint64(binary.LittleEndian.Uint32(header))
		offset, err := core1SegmentOffset(c, globals, element.Offset, constExprElementOffset, c.tableDef(int(element.TableIndex)).Addr64)
		if err != nil {
			return fmt.Errorf("active element segment %d offset: %w", seg, err)
		}
		if offset > size || uint64(len(element.Values)) > size-offset {
			return fmt.Errorf("active element segment %d out of bounds on table %d: offset %d + length %d > table size %d", seg, element.TableIndex, offset, len(element.Values), size)
		}
	}
	return nil
}
