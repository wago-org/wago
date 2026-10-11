//go:build !(linux && riscv64 && wago_rv64_experimental) || tinygo || wago_target_tinygo

package wago

func rv64GCStructBasicExecutionCode(*Compiled) bool  { return false }
func rv64GCStructBasic(*Compiled) bool               { return false }
func railshotRestoreGCStructBasicArtifact(*Compiled) {}
