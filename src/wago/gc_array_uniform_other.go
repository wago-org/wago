//go:build !linux || !riscv64 || tinygo || wago_target_tinygo

package wago

func rv64GCArrayUniform(*Compiled) bool              { return false }
func rv64GCArrayUniformExecutionCode(*Compiled) bool { return false }
