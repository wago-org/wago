//go:build !linux || !riscv64 || !wago_rv64_experimental || tinygo || wago_target_tinygo

package wago

func rv64GCRefTestConcrete(*Compiled) bool               { return false }
func rv64GCRefTestConcreteExecutionCode(*Compiled) bool  { return false }
func railshotRestoreGCRefTestConcreteArtifact(*Compiled) {}
func rv64GCRefTestAbstract(*Compiled) bool               { return false }
func rv64GCRefTestAbstractExecutionCode(*Compiled) bool  { return false }
func railshotRestoreGCRefTestAbstractArtifact(*Compiled) {}
func rv64GCExtern(*Compiled) bool                        { return false }
func rv64GCExternExecutionCode(*Compiled) bool           { return false }
func railshotRestoreGCExternArtifact(*Compiled)          {}
func rv64GCRefCastAbstract(*Compiled) bool               { return false }
func rv64GCRefCastAbstractExecutionCode(*Compiled) bool  { return false }
func railshotRestoreGCRefCastAbstractArtifact(*Compiled) {}
func rv64GCRefCastConcrete(*Compiled) bool               { return false }
func rv64GCRefCastConcreteExecutionCode(*Compiled) bool  { return false }
func railshotRestoreGCRefCastConcreteArtifact(*Compiled) {}
