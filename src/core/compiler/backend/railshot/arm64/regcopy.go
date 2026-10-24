//go:build arm64

package arm64

// Parallel register-move resolution (WARP's RegisterCopyResolver): placing N
// values, each already live in some register, into their target registers is a
// *parallel* move — a target may still hold a value another move needs — and the
// dependency graph can contain cycles. resolveRegMoves orders the copies so no
// move overwrites a still-needed source, breaking pure cycles with a swap. Used
// to marshal call arguments into the register-ABI argument registers.

// regMove requests dst = src. dst == src is a no-op.
type regMove struct{ dst, src Reg }

const maxMachineWindow = 24

type machineOpKind uint8

const (
	machineMove machineOpKind = iota
	machineSwap
)

type machineOp struct {
	kind     machineOpKind
	dst, src Reg
}

// machineWindow is the first fixed-capacity symbolic machine-operation seam.
// It currently covers resolved ABI/entry register shuffles, whose end is an
// explicit call or body boundary. Capacity exhaustion flushes in program order;
// no correctness or compile work depends on retaining the whole shuffle.
type machineWindow struct {
	ops       [maxMachineWindow]machineOp
	n         uint8
	move      func(dst, src Reg)
	swap      func(a, b Reg)
	swapChain func(a, b, c Reg)
	rewrites  int
}
