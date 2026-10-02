//go:build !wago_regalloccheck

package arm64

import "github.com/wago-org/wago/internal/regalloccheck"

const regallocCheckEnabled = false

// Keep this first in Asm: an empty trailing field can grow a Go struct.
type regallocState struct{}

func (*Asm) ObserveRegalloc(func(regalloccheck.Effect)) func(regalloccheck.Effect) { return nil }
func (*Asm) regallocCopy(Reg, Reg, bool, int)                                      {}
func (*Asm) regallocLoad(Reg, Reg, int32, bool, int)                               {}
func (*Asm) regallocStore(Reg, int32, Reg, bool, int)                              {}
func regallocWidth(wide bool) int                                                  { return 0 }

func (*Asm) regallocCrossCopy(Reg, Reg, bool, int) {}
func (*Asm) regallocKillFP(Reg)                    {}
