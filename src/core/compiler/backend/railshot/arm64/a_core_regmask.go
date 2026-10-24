//go:build arm64

package arm64

func maskOf(regs ...Reg) regMask {
	var m regMask
	for _, r := range regs {
		m |= 1 << uint(r)
	}
	return m
}

func (m regMask) has(r Reg) bool { return m&(1<<uint(r)) != 0 }

func (m regMask) add(r Reg) regMask {
	return m | 1<<uint(r)
}

func (m regMask) remove(r Reg) regMask { return m &^ (1 << uint(r)) }

func (m regMask) union(o regMask) regMask {
	return m | o
}
