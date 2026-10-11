//go:build !(linux && riscv64 && wago_rv64_experimental) || tinygo || wago_target_tinygo

package wago

func rv64GCArrayInitElem(*Compiled) bool               { return false }
func rv64GCArrayInitElemExecutionCode(*Compiled) bool  { return false }
func railshotRestoreGCArrayInitElemArtifact(*Compiled) {}
