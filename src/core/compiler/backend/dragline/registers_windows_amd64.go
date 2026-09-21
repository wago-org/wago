package dragline

import "github.com/wago-org/wago/src/core/encoder/amd64"

// Windows requires XMM6-XMM15 to remain nonvolatile at the platform boundary.
// Retain the low volatile bank and the established high scratch bank there.
var amd64FPRRegisters = [...]amd64.Reg{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}
var amd64ScalarFPRRegisters = amd64FPRRegisters
