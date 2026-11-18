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
func (s *streamState) initializeRegisterHomes(selected [streamMaxHomes]uint16, nParams int) {
	// Fixed division/shift/bulk/poll scratch registers are never home candidates.
	baseCandidates := [...]uint8{uint8(R12), uint8(R13), uint8(R14), uint8(R10), uint8(R11)}
	candidates := baseCandidates[:]
	limit := 2
	var candidateStorage [16]uint8
	if s.residency != nil && streamingWideHomesEnabled {
		var poolStorage [16]Reg
		f := s.target
		regABI := f.ft != nil && f.opt(optRegABI) && sigFitsRegABI(f.ft)
		pool := gpPinPool(poolStorage[:0], regABI, nParams, true, f.opt(optEntryArgPins))
		candidates = candidateStorage[:0]
		for _, r := range pool {
			candidates = append(candidates, uint8(r))
		}
		limit = min(streamMaxHomes, gpPinLimit(regMask(s.reserved), 4))
	}
	count := 0
	for k, n := range selected {
		if count >= limit {
			break
		}
		if n == 0 || int(n) > len(s.locals) {
			continue
		}
		for _, reg := range candidates {
			if s.reserved&(uint64(1)<<reg) != 0 || (s.residency != nil && s.residency.registers[reg] != 0) {
				continue
			}
			if s.residency == nil {
				s.reserved |= uint64(1) << reg
			}
			s.homeLocals[k], s.homeRegs[k] = n, reg
			local := int(n) - 1
			if local < nParams {
				s.target.Load(reg, s.target.LocalOffset(local), s.widths[local])
			} else {
				s.target.Constant(reg, 0, s.widths[local])
			}
			s.release(s.locals[local])
			if s.residency != nil {
				cell := s.newBacking(reg, s.widths[local])
				s.residency.homes[k] = cell
				s.locals[local] = s.borrowBacking(cell, local)
				s.target.stats.peep("stream-residency-home")
			} else {
				s.locals[local] = s.add(streamNode{kind: streamBorrow, slot: int32(local), home: s.localHomeTag(local), wide: s.widths[local], refs: 1})
			}
			count++
			s.target.stats.peep("stream-local-home")
			break
		}
	}
}
func (s *streamState) commitRegisterHomes() {
	if s.residency != nil {
		s.commitBackingHomes()
		return
	}
	// Capture both inputs before changing either home. Ordinary owners never
	// occupy home registers, and tagged spills always create genuine frame copies.
	var sources [streamMaxHomes]uint8
	var changed [streamMaxHomes]bool
	saved := s.reserved
	for k, n := range s.homeLocals {
		if n == 0 {
			continue
		}
		local := int(n) - 1
		id := s.locals[local]
		if id == 0 {
			continue
		}
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
