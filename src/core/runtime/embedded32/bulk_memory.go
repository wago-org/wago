package embedded32

// DataSegmentInit describes one module data segment in original index order.
// Active segments start dropped after instantiation; passive segments remain
// available to memory.init until data.drop.
type DataSegmentInit struct {
	Bytes   []byte
	Dropped bool
}

type DataStore struct {
	segments [][]byte
	dropped  []bool
}

func NewDataStore(inits []DataSegmentInit) *DataStore {
	s := &DataStore{segments: make([][]byte, len(inits)), dropped: make([]bool, len(inits))}
	for i := range inits {
		s.segments[i] = inits[i].Bytes
		s.dropped[i] = inits[i].Dropped
	}
	return s
}

func (s *DataStore) Drop(index uint32) bool {
	if s == nil || uint64(index) >= uint64(len(s.segments)) {
		return false
	}
	s.dropped[index] = true
	return true
}

func (s *DataStore) Init(memory *LinearMemory, index, dst, src, n uint32) Trap {
	if s == nil || memory == nil || uint64(index) >= uint64(len(s.segments)) {
		return TrapMemoryOutOfBounds
	}
	segment := s.segments[index]
	length := uint64(len(segment))
	if s.dropped[index] {
		length = 0
	}
	if uint64(src)+uint64(n) > length || !memory.Bounds(dst, n) {
		return TrapMemoryOutOfBounds
	}
	copy(memory.backing[dst:dst+n], segment[src:src+n])
	return TrapNone
}

func (m *LinearMemory) Copy(dst, src, n uint32) Trap {
	if m == nil || !m.Bounds(dst, n) || !m.Bounds(src, n) {
		return TrapMemoryOutOfBounds
	}
	copy(m.backing[dst:dst+n], m.backing[src:src+n])
	return TrapNone
}

func (m *LinearMemory) Fill(dst uint32, value byte, n uint32) Trap {
	if m == nil || !m.Bounds(dst, n) {
		return TrapMemoryOutOfBounds
	}
	for i := uint32(0); i < n; i++ {
		m.backing[dst+i] = value
	}
	return TrapNone
}
