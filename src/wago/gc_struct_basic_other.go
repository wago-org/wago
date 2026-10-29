//go:build !(linux && riscv64) || tinygo || wago_target_tinygo

package wago

func rv64GCStructBasicExecutionCode(*Compiled) bool { return false }
func rv64GCStructBasic(*Compiled) bool              { return false }
