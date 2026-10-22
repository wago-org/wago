//go:build riscv64 && !tinygo

package main

import "github.com/wago-org/wago/codegen"

// CustomSignature registration requires nonnil target machine code. Until RV64
// has genuine custom lowering, the example returns that registration error
// before compiling or entering a guest; it cannot select a foreign ISA.
func zeroCodegen() codegen.Lowering    { return nil }
func discardCodegen() codegen.Lowering { return nil }
