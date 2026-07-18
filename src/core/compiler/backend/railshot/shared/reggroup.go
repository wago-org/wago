package shared

// RegGroup is one atomically-owned machine value. Width is one for 32-bit
// scalars, two for i64/f64 pairs, and four for v128 quads. Register numbers are
// architecture-owned identifiers; this package does not assign ABI meaning.
type RegGroup struct {
	Regs  [4]uint8
	Width uint8
	owner uint8
}

func (g RegGroup) Valid() bool { return g.owner != 0 && (g.Width == 1 || g.Width == 2 || g.Width == 4) }

// GroupAllocator tracks complete value ownership rather than independent
// registers. Its fixed storage keeps allocation bounded and allocation-free.
type GroupAllocator struct {
	pool       [32]uint8
	poolLen    uint8
	ownerByReg [32]uint8
	states     [33]groupState
	clock      uint64
}

type groupState struct {
	group RegGroup
	age   uint64
	live  bool
}

func NewGroupAllocator(regs []uint8) GroupAllocator {
	if len(regs) > 32 {
		panic("railshot: register group pool exceeds 32 registers")
	}
	var a GroupAllocator
	var seen uint32
	for i, r := range regs {
		if r >= 32 || seen&(uint32(1)<<r) != 0 {
			panic("railshot: invalid or duplicate register in group pool")
		}
		seen |= uint32(1) << r
		a.pool[i] = r
	}
	a.poolLen = uint8(len(regs))
	return a
}

func validGroupWidth(width uint8) bool { return width == 1 || width == 2 || width == 4 }

// Alloc acquires the first complete width-register group available in pool
// order. It never acquires a partial group.
func (a *GroupAllocator) Alloc(width uint8) (RegGroup, bool) {
	if !validGroupWidth(width) {
		return RegGroup{}, false
	}
	var regs [4]uint8
	n := uint8(0)
	for i := uint8(0); i < a.poolLen && n < width; i++ {
		r := a.pool[i]
		if a.ownerByReg[r] == 0 {
			regs[n] = r
			n++
		}
	}
	if n != width {
		return RegGroup{}, false
	}
	return a.acquire(regs, width)
}

// Acquire atomically reserves an exact register group, for example an incoming
// ABI pair or quad. No ownership changes if any requested register is busy.
func (a *GroupAllocator) Acquire(regs [4]uint8, width uint8) (RegGroup, bool) {
	if !validGroupWidth(width) {
		return RegGroup{}, false
	}
	var seen uint32
	for i := uint8(0); i < width; i++ {
		r := regs[i]
		if r >= 32 || seen&(uint32(1)<<r) != 0 || a.ownerByReg[r] != 0 {
			return RegGroup{}, false
		}
		seen |= uint32(1) << r
	}
	return a.acquire(regs, width)
}

func (a *GroupAllocator) acquire(regs [4]uint8, width uint8) (RegGroup, bool) {
	owner := uint8(0)
	for i := uint8(1); i < uint8(len(a.states)); i++ {
		if !a.states[i].live {
			owner = i
			break
		}
	}
	if owner == 0 {
		return RegGroup{}, false
	}
	g := RegGroup{Regs: regs, Width: width, owner: owner}
	a.clock++
	a.states[owner] = groupState{group: g, age: a.clock, live: true}
	for i := uint8(0); i < width; i++ {
		a.ownerByReg[regs[i]] = owner
	}
	return g, true
}

// Release frees a complete owned group. Stale, forged, or partial groups are
// rejected without changing allocator state.
func (a *GroupAllocator) Release(g RegGroup) bool {
	if !g.Valid() || int(g.owner) >= len(a.states) {
		return false
	}
	s := a.states[g.owner]
	if !s.live || s.group != g {
		return false
	}
	for i := uint8(0); i < g.Width; i++ {
		if a.ownerByReg[g.Regs[i]] != g.owner {
			return false
		}
	}
	for i := uint8(0); i < g.Width; i++ {
		a.ownerByReg[g.Regs[i]] = 0
	}
	a.states[g.owner] = groupState{}
	return true
}

// Touch marks a complete group as recently used for bounded spill selection.
func (a *GroupAllocator) Touch(g RegGroup) bool {
	if !a.Owns(g) {
		return false
	}
	a.clock++
	s := a.states[g.owner]
	s.age = a.clock
	a.states[g.owner] = s
	return true
}

func (a *GroupAllocator) Owns(g RegGroup) bool {
	if !g.Valid() || int(g.owner) >= len(a.states) {
		return false
	}
	s := a.states[g.owner]
	if !s.live || s.group != g {
		return false
	}
	for i := uint8(0); i < g.Width; i++ {
		if a.ownerByReg[g.Regs[i]] != g.owner {
			return false
		}
	}
	return true
}

// Victim returns the least-recently-used complete group not intersecting the
// excluded register mask. Selection never exposes an individual pair/quad word.
func (a *GroupAllocator) Victim(excluded uint32) (RegGroup, bool) {
	var best groupState
	for i := 1; i < len(a.states); i++ {
		s := a.states[i]
		if !s.live {
			continue
		}
		blocked := false
		for j := uint8(0); j < s.group.Width; j++ {
			if excluded&(uint32(1)<<s.group.Regs[j]) != 0 {
				blocked = true
				break
			}
		}
		if !blocked && (!best.live || s.age < best.age) {
			best = s
		}
	}
	return best.group, best.live
}

func (a *GroupAllocator) FreeRegisters() int {
	n := 0
	for i := uint8(0); i < a.poolLen; i++ {
		if a.ownerByReg[a.pool[i]] == 0 {
			n++
		}
	}
	return n
}
