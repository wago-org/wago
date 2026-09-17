package railssa

import (
	"fmt"
	"slices"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

type RegionID uint16

const NoRegion RegionID = ^RegionID(0)

const (
	maxPrepassLocalEvents   = 1 << 20
	compactPrepassBodyBytes = 16 << 10
)

// Region is one structured Wasm control region. Instruction indexes refer to
// StackFunc.Instrs and all local sets are stored in flat CSR-style slabs.
type Region struct {
	Parent RegionID
	Kind   wasm.InstrKind

	StartInstr uint32
	ElseInstr  uint32
	EndInstr   uint32

	AssignedStart uint32
	AssignedCount uint16
	MergeStart    uint32
	MergeCount    uint16
	LoopStart     uint32
	LoopCount     uint16

	StackDepth  uint32
	MaxPressure uint32
	LoopDepth   uint8
	ParamArity  uint16
	ResultArity uint16
}

func (r Region) AssignedLocals(f *StackFunc) []uint32 {
	return f.RegionLocals[r.AssignedStart : r.AssignedStart+uint32(r.AssignedCount)]
}

func (r Region) MergeLocals(f *StackFunc) []uint32 {
	return f.MergeLocals[r.MergeStart : r.MergeStart+uint32(r.MergeCount)]
}

func (r Region) LoopLocals(f *StackFunc) []uint32 {
	return f.LoopLocals[r.LoopStart : r.LoopStart+uint32(r.LoopCount)]
}

// regionLocalEvent packs a 16-bit region, 32-bit local, and read/write flags
// into one word. Keeping this scratch record at eight bytes matters on deeply
// nested functions where one local access is attributed to several regions.
type regionLocalEvent uint64

const (
	regionLocalRead  regionLocalEvent = 1 << 48
	regionLocalWrite regionLocalEvent = 1 << 49
)

func makeRegionLocalEvent(region RegionID, local uint32, read, write bool) regionLocalEvent {
	event := regionLocalEvent(region) | regionLocalEvent(local)<<16
	if read {
		event |= regionLocalRead
	}
	if write {
		event |= regionLocalWrite
	}
	return event
}

func (e regionLocalEvent) region() RegionID { return RegionID(e) }
func (e regionLocalEvent) local() uint32    { return uint32(e >> 16) }
func (e regionLocalEvent) reads() bool      { return e&regionLocalRead != 0 }
func (e regionLocalEvent) writes() bool     { return e&regionLocalWrite != 0 }

type parseControl struct {
	region          RegionID
	kind            wasm.InstrKind
	baseDepth       int
	paramArity      uint16
	resultArity     uint16
	parentReachable bool
	endReached      bool
	seenElse        bool
}

func recordRegionLocal(events *[]regionLocalEvent, controls []parseControl, local uint32, read, write, compact bool) error {
	count := len(controls)
	if compact && count != 0 {
		count = 1
	}
	if len(*events)+count > maxPrepassLocalEvents {
		return &BudgetError{Resource: "structured prepass region-local events", Required: uint64(len(*events) + count), Limit: maxPrepassLocalEvents}
	}
	if compact && len(controls) != 0 {
		*events = append(*events, makeRegionLocalEvent(controls[len(controls)-1].region, local, read, write))
		return nil
	}
	for i := range controls {
		*events = append(*events, makeRegionLocalEvent(controls[i].region, local, read, write))
	}
	return nil
}

func expandCompactRegionLocals(f *StackFunc, events []regionLocalEvent) ([]regionLocalEvent, error) {
	slices.SortFunc(events, compareRegionLocalEvent)
	events = mergeRegionLocalEvents(events)
	states := make(map[uint64]uint8, len(events))
	queue := events
	for _, event := range events {
		states[regionLocalKey(event.region(), event.local())] = regionLocalFlags(event)
	}
	for head := 0; head < len(queue); head++ {
		event := queue[head]
		region := event.region()
		if int(region) >= len(f.Regions) || f.Regions[region].Parent == NoRegion {
			continue
		}
		parent := f.Regions[region].Parent
		key := regionLocalKey(parent, event.local())
		flags := states[regionLocalKey(region, event.local())]
		combined := states[key] | flags
		if combined == states[key] {
			continue
		}
		if _, exists := states[key]; !exists && len(states) >= maxPrepassLocalEvents {
			return nil, &BudgetError{Resource: "structured prepass unique region-local events", Required: uint64(len(states)) + 1, Limit: maxPrepassLocalEvents}
		}
		states[key] = combined
		queue = append(queue, makeRegionLocalEvent(parent, event.local(), combined&1 != 0, combined&2 != 0))
	}
	events = queue[:0]
	for key, flags := range states {
		events = append(events, makeRegionLocalEvent(RegionID(key>>32), uint32(key), flags&1 != 0, flags&2 != 0))
	}
	slices.SortFunc(events, compareRegionLocalEvent)
	return events, nil
}

func regionLocalKey(region RegionID, local uint32) uint64 {
	return uint64(region)<<32 | uint64(local)
}

func regionLocalFlags(event regionLocalEvent) uint8 {
	var flags uint8
	if event.reads() {
		flags |= 1
	}
	if event.writes() {
		flags |= 2
	}
	return flags
}

func compareRegionLocalEvent(a, b regionLocalEvent) int {
	if a.region() < b.region() || a.region() == b.region() && a.local() < b.local() {
		return -1
	}
	if a.region() == b.region() && a.local() == b.local() {
		return 0
	}
	return 1
}

func mergeRegionLocalEvents(events []regionLocalEvent) []regionLocalEvent {
	out := events[:0]
	for i := 0; i < len(events); {
		event := events[i]
		read, write := event.reads(), event.writes()
		j := i + 1
		for j < len(events) && events[j].region() == event.region() && events[j].local() == event.local() {
			read = read || events[j].reads()
			write = write || events[j].writes()
			j++
		}
		out = append(out, makeRegionLocalEvent(event.region(), event.local(), read, write))
		i = j
	}
	return out
}

func finalizePrepass(f *StackFunc, events []regionLocalEvent, lastLocalUse []uint32, compact bool) error {
	if compact {
		var err error
		events, err = expandCompactRegionLocals(f, events)
		if err != nil {
			return err
		}
	} else {
		slices.SortFunc(events, compareRegionLocalEvent)
	}
	for i := 0; i < len(events); {
		event := events[i]
		read, write := event.reads(), event.writes()
		j := i + 1
		for j < len(events) && events[j].region() == event.region() && events[j].local() == event.local() {
			read = read || events[j].reads()
			write = write || events[j].writes()
			j++
		}
		region := &f.Regions[event.region()]
		if write {
			if region.AssignedCount == ^uint16(0) {
				return fmt.Errorf("structured region %d exceeds local-set count", event.region())
			}
			if region.AssignedCount == 0 {
				region.AssignedStart = uint32(len(f.RegionLocals))
			}
			f.RegionLocals = append(f.RegionLocals, event.local())
			region.AssignedCount++
			if int(event.local()) < len(lastLocalUse) && lastLocalUse[event.local()] > region.EndInstr {
				if region.MergeCount == 0 {
					region.MergeStart = uint32(len(f.MergeLocals))
				}
				f.MergeLocals = append(f.MergeLocals, event.local())
				region.MergeCount++
			}
		}
		if write && read && region.Kind == wasm.InstrLoop {
			if region.LoopCount == 0 {
				region.LoopStart = uint32(len(f.LoopLocals))
			}
			f.LoopLocals = append(f.LoopLocals, event.local())
			region.LoopCount++
		}
		i = j
	}
	return nil
}
