//go:build !linux || !riscv64 || tinygo || wago_target_tinygo

package wago

func rv64GCArrayFill(*Compiled) bool               { return false }
func rv64GCArrayFillExecutionCode(*Compiled) bool  { return false }
func railshotRestoreGCArrayFillArtifact(*Compiled) {}
