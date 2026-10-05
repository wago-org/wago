//go:build !wago_regalloccheck

package amd64

import "github.com/wago-org/wago/internal/regalloccheck"

// Guard each hook with this constant so ordinary builds erase argument setup too.
const regallocCheckEnabled = false

// Keep this first in Asm: an empty trailing field can grow a Go struct.
type regallocState struct{}

func regallocCall(*Asm) {}

func regallocScalarFP(*Asm, byte, byte, byte, Reg, Reg, bool) {}

func regallocVexScalarFP(*Asm, byte, byte, byte, Reg, Reg, byte) {}

func (*Asm) ObserveRegalloc(func(regalloccheck.Effect)) func(regalloccheck.Effect) { return nil }
func (*Asm) regallocCopy(Reg, Reg, bool, int)                                      {}
func (*Asm) regallocSwap(Reg, Reg, int)                                            {}
func (*Asm) regallocLoad(Reg, Reg, int32, bool, int)                               {}
func (*Asm) regallocStore(Reg, int32, Reg, bool, int)                              {}
func regallocWidth(wide bool) int                                                  { return 0 }

func (*Asm) regallocRead(Reg, int32, int) {}

func (*Asm) regallocCrossCopy(Reg, Reg, bool, int) {}
func (*Asm) regallocKillFP(Reg)                    {}

func (*Asm) ObserveGPWrites(func(uint32)) func(uint32)      { return nil }
func (*Asm) regallocGPWrite(uint32)                         {}
func (*Asm) regallocGPRR(byte, Reg, Reg, bool)              {}
func (*Asm) regallocGPMem(byte, Reg, bool)                  {}
func (*Asm) regallocGPSSE(byte, byte, byte, Reg, Reg, bool) {}
func (*Asm) regallocGPVEX(byte, byte, byte, Reg, Reg, bool) {}
