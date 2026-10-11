//go:build !linux || !riscv64 || !wago_rv64_experimental || tinygo || wago_target_tinygo

package wago

func rv64GCArrayCopy(*Compiled) bool               { return false }
func rv64GCArrayCopyExecutionCode(*Compiled) bool  { return false }
func railshotRestoreGCArrayCopyArtifact(*Compiled) {}
