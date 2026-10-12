//go:build !linux || !riscv64 || !wago_rv64_experimental || tinygo || wago_target_tinygo

package wago

func railshotUsesGuestGlobalClearHelper(*Compiled) bool { return false }

func (*Instance) dispatchGuestGlobalClear(uint32, []uint64, []uint64) bool { return false }
