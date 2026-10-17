//go:build !(linux && riscv64) || tinygo || wago_target_tinygo

package wago

func rv64GCStructGetExecutionCode(*Compiled) bool { return false }
func rv64GCStructGet(*Compiled) bool              { return false }
