//go:build !(linux && riscv64) || tinygo || wago_target_tinygo

package wago

func rv64GCStructNullExecutionCode(*Compiled) bool { return false }
func rv64GCStructNull(*Compiled) bool              { return false }
