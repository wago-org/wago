//go:build amd64 && !wago_experimental_cooperative_linear_sum

package amd64

const cooperativeLinearSumEnabled = false

type cooperativeHeaderState struct{}

func (*cooperativeHeaderState) setCooperativeHeader(int, uint32) {}
func (*cooperativeHeaderState) cooperativeHeaderEnd() int        { return -1 }
func (*cooperativeHeaderState) cooperativeHeaderPC() uint32      { return 0 }
