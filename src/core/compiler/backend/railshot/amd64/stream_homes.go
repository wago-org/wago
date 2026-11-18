//go:build amd64 && !tinygo && !wago_profile

package amd64

// The tag distinguishes a register-backed agreement from a valid frame copy.
const streamRegisterHome uint16 = 1 << 15

func (s *streamState) localHomeReg(local int) (uint8, bool) {
	for k, n := range s.homeLocals {
		if n != 0 && int(n)-1 == local {
			return s.homeRegs[k], true
		}
	}
	return 0, false
}
func (s *streamState) localHomeTag(local int) uint16 {
	tag := uint16(local + 1)
	if _, ok := s.localHomeReg(local); ok {
		tag |= streamRegisterHome
	}
	return tag
}
func (s *streamState) initializeRegisterHomes(selected [2]uint16, nParams int) {
	// Fixed division/shift/bulk/poll scratch registers are never home candidates.
	candidates := [...]uint8{uint8(R12), uint8(R13), uint8(R14), uint8(R10), uint8(R11)}
	for k, n := range selected {
		if n == 0 || int(n) > len(s.locals) {
			continue
		}
		for _, reg := range candidates {
			if s.reserved&(uint64(1)<<reg) != 0 {
				continue
			}
			s.reserved |= uint64(1) << reg
			s.homeLocals[k], s.homeRegs[k] = n, reg
			local := int(n) - 1
			if local < nParams {
				s.target.Load(reg, s.target.LocalOffset(local), s.widths[local])
			} else {
				s.target.Constant(reg, 0, s.widths[local])
			}
			s.release(s.locals[local])
			s.locals[local] = s.add(streamNode{kind: streamBorrow, slot: int32(local), home: s.localHomeTag(local), wide: s.widths[local], refs: 1})
			s.target.stats.peep("stream-local-home")
			break
		}
	}
}
func (s *streamState) commitRegisterHomes() {
	// Capture both inputs before changing either home. Ordinary owners never
	// occupy home registers, and tagged spills always create genuine frame copies.
	var sources [2]uint8
	var changed [2]bool
	saved := s.reserved
	for k, n := range s.homeLocals {
		if n == 0 {
			continue
		}
		local := int(n) - 1
		id := s.locals[local]
		if s.node(id).home == s.localHomeTag(local) {
			continue
		}
		sources[k] = s.materialize(id, 0)
		s.reserved |= uint64(1) << sources[k]
		changed[k] = true
	}
	for k, yes := range changed {
		if yes {
			s.target.Move(s.homeRegs[k], sources[k], s.widths[int(s.homeLocals[k])-1])
		}
	}
	s.reserved = saved
}
