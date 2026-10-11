//go:build !(linux && riscv64 && wago_rv64_experimental) || tinygo || wago_target_tinygo

package wago

func rv64GCStructNullExecutionCode(*Compiled) bool { return false }
func rv64GCStructNull(*Compiled) bool              { return false }
