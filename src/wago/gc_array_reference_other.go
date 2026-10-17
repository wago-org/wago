//go:build !linux || !riscv64 || tinygo || wago_target_tinygo

package wago

func rv64GCArrayReference(*Compiled) bool               { return false }
func rv64GCArrayReferenceExecutionCode(*Compiled) bool  { return false }
func railshotRestoreGCArrayReferenceArtifact(*Compiled) {}
