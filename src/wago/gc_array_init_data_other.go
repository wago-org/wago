//go:build !(linux && riscv64) || tinygo || wago_target_tinygo

package wago

func rv64GCArrayInitData(*Compiled) bool              { return false }
func rv64GCArrayInitDataExecutionCode(*Compiled) bool { return false }
