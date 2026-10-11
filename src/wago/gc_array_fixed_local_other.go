//go:build !linux || !riscv64 || !wago_rv64_experimental || tinygo || wago_target_tinygo

package wago

func rv64GCArrayFixedLocal(*Compiled) bool              { return false }
func rv64GCArrayFixedLocalExecutionCode(*Compiled) bool { return false }
