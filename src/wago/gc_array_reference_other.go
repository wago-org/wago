//go:build !linux || !riscv64 || !wago_rv64_experimental || tinygo || wago_target_tinygo

package wago

func rv64GCArrayReference(*Compiled) bool               { return false }
func rv64GCArrayReferenceExecutionCode(*Compiled) bool  { return false }
func railshotRestoreGCArrayReferenceArtifact(*Compiled) {}
