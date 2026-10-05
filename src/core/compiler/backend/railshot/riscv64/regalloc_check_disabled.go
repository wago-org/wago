//go:build linux && !tinygo && !wago_regalloccheck

package riscv64

import enc "github.com/wago-org/wago/src/core/encoder/riscv64"

const regallocCheckEnabled = false

type regallocFnState struct{}
type regallocGPWriteMask struct{}

func (*target) checkImmutable(enc.Reg)                     {}
func (*target) checkEndLifetimes()                         {}
func (*target) checkTerminalGPWrites() regallocGPWriteMask { return regallocGPWriteMask{} }
func (*target) checkRestoreGPWrites(regallocGPWriteMask)   {}
