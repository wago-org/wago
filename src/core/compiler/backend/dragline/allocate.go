package dragline

import (
	"fmt"
	"sort"

	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railssa"
)

type locationKind uint8

const (
	locationInvalid locationKind = iota
	locationRegister
	locationSpill
)

type location struct {
	kind  locationKind
	index uint16
}

type allocation struct {
	values     []location
	frameBytes uint32
	peakBytes  uint64
}

type activeValue struct {
	value railssa.ValueID
	end   uint32
	reg   uint16
}

// allocateLinear performs the MVP's deterministic splitting baseline. Values
// that do not fit in the bounded register set receive dense spill slots.
func allocateLinear(fn *railssa.Func, registerCount int) (allocation, error) {
	if fn == nil || registerCount <= 0 || len(fn.Params) > registerCount {
		return allocation{}, fmt.Errorf("dragline: allocator cannot place %d parameters in %d registers", len(fn.Params), registerCount)
	}
	lastUse := make([]uint32, len(fn.Values))
	for id := range fn.Values {
		lastUse[id] = uint32(id)
		value := &fn.Values[id]
		start := int(value.Args.Start)
		for _, arg := range fn.Args[start : start+int(value.Args.Len)] {
			if int(arg) >= len(lastUse) {
				return allocation{}, fmt.Errorf("dragline: allocator saw invalid value %d", arg)
			}
			lastUse[arg] = uint32(id)
		}
	}
	if len(fn.Results) == 1 {
		lastUse[fn.Result] = uint32(len(fn.Values))
	}
	out := allocation{values: make([]location, len(fn.Values))}
	free := make([]bool, registerCount)
	for i := range free {
		free[i] = true
	}
	active := make([]activeValue, 0, registerCount)
	expire := func(position uint32) {
		kept := active[:0]
		for _, item := range active {
			if item.end < position {
				free[item.reg] = true
			} else {
				kept = append(kept, item)
			}
		}
		active = kept
	}
	spillCount := uint16(0)
	for id := range fn.Values {
		position := uint32(id)
		expire(position)
		reg := -1
		if id < len(fn.Params) {
			reg = id
			if !free[reg] {
				return allocation{}, fmt.Errorf("dragline: fixed parameter register %d is unavailable", reg)
			}
		} else {
			for candidate, available := range free {
				if available {
					reg = candidate
					break
				}
			}
		}
		if reg >= 0 {
			free[reg] = false
			out.values[id] = location{kind: locationRegister, index: uint16(reg)}
			active = append(active, activeValue{value: railssa.ValueID(id), end: lastUse[id], reg: uint16(reg)})
			sort.Slice(active, func(i, j int) bool { return active[i].end < active[j].end })
		} else {
			out.values[id] = location{kind: locationSpill, index: spillCount}
			spillCount++
		}
	}
	bytes := uint32(spillCount) * 8
	out.frameBytes = (bytes + 15) &^ 15
	out.peakBytes = sliceBytes(lastUse) + sliceBytes(out.values) + sliceBytes(free) + sliceBytes(active)
	return out, nil
}
