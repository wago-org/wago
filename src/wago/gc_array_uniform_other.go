//go:build !linux || !riscv64 || !wago_rv64_experimental || tinygo || wago_target_tinygo

package wago

func rv64GCArrayUniform(*Compiled) bool              { return false }
func rv64GCArrayUniformExecutionCode(*Compiled) bool { return false }
