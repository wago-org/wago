//go:build !linux || !riscv64 || tinygo || wago_target_tinygo

package wago

func rv64OfficialGCRefEq(*Compiled) bool { return false }
