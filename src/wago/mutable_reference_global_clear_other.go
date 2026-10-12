//go:build !linux || !riscv64 || !wago_rv64_experimental || tinygo || wago_target_tinygo

package wago

func railshotUsesGuestGlobalStoreHelper(*Compiled) bool { return false }

func (*Instance) dispatchGuestGlobalStore(uint32, []uint64, []uint64) bool { return false }
