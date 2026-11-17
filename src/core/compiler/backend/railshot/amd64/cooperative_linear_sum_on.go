//go:build amd64 && wago_experimental_cooperative_linear_sum

package amd64

const cooperativeLinearSumEnabled = true

type cooperativeHeaderState struct {
	end int
	pc  uint32
}

func (s *cooperativeHeaderState) setCooperativeHeader(end int, pc uint32) { s.end, s.pc = end, pc }
func (s *cooperativeHeaderState) cooperativeHeaderEnd() int               { return s.end }
func (s *cooperativeHeaderState) cooperativeHeaderPC() uint32             { return s.pc }
