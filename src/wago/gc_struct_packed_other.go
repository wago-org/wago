//go:build !(linux && riscv64 && wago_rv64_experimental) || tinygo || wago_target_tinygo

package wago

func rv64GCStructPackedExecutionCode(*Compiled) bool  { return false }
func rv64GCStructPacked(*Compiled) bool               { return false }
func railshotRestoreGCStructPackedArtifact(*Compiled) {}
