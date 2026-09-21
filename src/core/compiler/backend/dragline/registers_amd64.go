//go:build amd64 && !windows

package dragline

import "github.com/wago-org/wago/src/core/encoder/amd64"

// Keep the fixed SIMD-lowering scratch bank last. The planner can then reserve
// XMM5, XMM4-XMM5, or XMM3-XMM5 by shortening the allocatable prefix without
// also discarding the independent XMM6-XMM12 value bank.
var amd64FPRRegisters = [...]amd64.Reg{0, 1, 2, 6, 7, 8, 9, 10, 11, 12, 3, 4, 5}

// Scalar lowering reserves XMM13-XMM15 and has no fixed XMM3-XMM5 scratch
// contract, so retain physical order for private-ABI arguments and clobbers.
var amd64ScalarFPRRegisters = [...]amd64.Reg{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
