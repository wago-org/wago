//go:build amd64

package amd64

import (
	"unsafe"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

// operandArena owns stable RW storage for operand nodes. Nodes contain only
// scalar storage and links to other nodes in the same worker-owned arena;
// plugin-owned Go values remain in stack.cold, which is scanned normally.
type operandArena = coreruntime.CodeBuffer

func operandArenaChunk(a *operandArena, capacity int) []elem {
	if a != nil {
		bytes := capacity * int(unsafe.Sizeof(elem{}))
		// Never grow this mapping: live expression links require stable addresses.
		if bytes <= len(a.Mapping())-len(a.Bytes()) {
			storage, err := a.AppendSpace(bytes)
			if err == nil {
				return unsafe.Slice((*elem)(unsafe.Pointer(unsafe.SliceData(storage))), capacity)[:0:capacity]
			}
		}
	}
	return make([]elem, 0, capacity)
}
