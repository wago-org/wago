//go:build !linux || !riscv64 || tinygo || wago_target_tinygo

package wago

func rv64GCRefTestTable(*Compiled) bool { return false }
