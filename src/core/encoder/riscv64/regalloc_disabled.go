//go:build !wago_regalloccheck

package riscv64

import "github.com/wago-org/wago/internal/regalloccheck"

const regallocCheckEnabled = false

// Leading empty compatibility state preserves the ordinary Asm layout.
type regallocState struct{}

func (*Asm) ObserveGPWrites(func(uint32)) func(uint32)                             { return nil }
func (*Asm) ObserveRegalloc(func(regalloccheck.Effect)) func(regalloccheck.Effect) { return nil }
func (*Asm) regallocDefinition(Reg, bool)                                          {}
func (*Asm) regallocFloatDefinition(FReg)                                          {}
func (*Asm) regallocImmediate(uint32, uint32, Reg, Reg, int32)                     {}
func (*Asm) regallocStore(Reg, Reg, int32, uint32)                                 {}

func (*Asm) regallocAtomic(Reg, Reg, bool, int) {}
