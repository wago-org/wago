//go:build !linux || !riscv64 || tinygo || wago_target_tinygo

package wago

func rv64GCArrayCopy(*Compiled) bool               { return false }
func rv64GCArrayCopyExecutionCode(*Compiled) bool  { return false }
func railshotRestoreGCArrayCopyArtifact(*Compiled) {}
