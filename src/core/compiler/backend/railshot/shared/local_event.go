package shared

// LocalEventLimit is the normal hard cap for the regional-residency prepass.
// Functions which exceed it keep their coarse hints and abandon detailed
// planning.
const LocalEventLimit = 32 * 1024

// LocalEventInitialCapacity bounds eager tape reservation. It covers the
// measured BLAKE/SWAR kernels without making a different large eligible body
// dictate module-wide scratch. Append still grows fail-soft up to the hard cap.
const LocalEventInitialCapacity = 1152

// NoLocal is stored on events which describe a boundary rather than a local.
const NoLocal = ^uint16(0)

// LocalEventKind identifies the small set of physical-planning facts retained
// by the local event prepass.
type LocalEventKind uint8

const (
	LocalEventRead LocalEventKind = iota + 1
	LocalEventDefine
	LocalEventBlock
	LocalEventLoop
	LocalEventIf
	LocalEventElse
	LocalEventEnd
	LocalEventBranch
	LocalEventCall
	LocalEventCollection
	LocalEventInvalidate
	LocalEventFixedReg
	LocalEventPressure
)

// LocalEvent is deliberately four bytes and pointer-free. Event order is the
// logical position, so the planner does not need to retain byte offsets.
type LocalEvent struct {
	Local uint16
	Depth uint8
	Kind  LocalEventKind
}

// LocalEventTape is worker-owned scratch. Reset reuses its backing storage
// across functions, and Append fails soft at the configured limit.
type LocalEventTape struct {
	Events   []LocalEvent
	Limit    int
	Overflow bool
}

func (t *LocalEventTape) Reset(limit int) {
	t.Events = t.Events[:0]
	t.Limit = limit
	t.Overflow = false
}

func (t *LocalEventTape) Append(kind LocalEventKind, local uint16, depth int) {
	if t.Limit <= 0 || len(t.Events) >= t.Limit {
		t.Overflow = true
		return
	}
	if depth > 255 {
		depth = 255
	} else if depth < 0 {
		depth = 0
	}
	t.Events = append(t.Events, LocalEvent{Local: local, Depth: uint8(depth), Kind: kind})
}

// FindLocalEventMeta finds a packed event summary in ordered key, value pairs.
// It is kept here so both native backends use the same bounded lookup.
func FindLocalEventMeta(pairs []uint32, key uint32) uint32 {
	lo, hi := 0, len(pairs)/2
	for lo < hi {
		mid := int(uint(lo+hi) >> 1)
		if pairs[mid*2] < key {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo < len(pairs)/2 && pairs[lo*2] == key {
		return pairs[lo*2+1]
	}
	return 0
}
