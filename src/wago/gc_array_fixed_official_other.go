//go:build !linux || !riscv64 || !wago_rv64_experimental || tinygo || wago_target_tinygo

package wago

func rv64GCArrayFixedOfficial(*Compiled) bool               { return false }
func rv64GCArrayFixedOfficialExecutionCode(*Compiled) bool  { return false }
func railshotRestoreGCArrayFixedOfficialArtifact(*Compiled) {}
