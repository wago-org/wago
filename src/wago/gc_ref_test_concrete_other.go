//go:build !linux || !riscv64 || !wago_rv64_experimental || tinygo || wago_target_tinygo

package wago

func rv64GCRefTestConcrete(*Compiled) bool                         { return false }
func rv64GCRefTestConcreteExecutionCode(*Compiled) bool            { return false }
func railshotRestoreGCRefTestConcreteArtifact(*Compiled)           {}
func rv64GCRefTestAbstract(*Compiled) bool                         { return false }
func rv64GCRefTestAbstractExecutionCode(*Compiled) bool            { return false }
func railshotRestoreGCRefTestAbstractArtifact(*Compiled)           {}
func rv64GCExtern(*Compiled) bool                                  { return false }
func rv64GCExternExecutionCode(*Compiled) bool                     { return false }
func railshotRestoreGCExternArtifact(*Compiled)                    {}
func rv64GCRefCastAbstract(*Compiled) bool                         { return false }
func rv64GCRefCastAbstractExecutionCode(*Compiled) bool            { return false }
func rv64GCBrOnCastAbstract(*Compiled) bool                        { return false }
func rv64GCBrOnCastAbstractExecutionCode(*Compiled) bool           { return false }
func rv64GCBrOnCastFailAbstract(*Compiled) bool                    { return false }
func rv64GCBrOnCastFailAbstractExecutionCode(*Compiled) bool       { return false }
func railshotRestoreGCBrOnCastAbstractArtifact(*Compiled)          {}
func railshotRestoreGCBrOnCastFailAbstractArtifact(*Compiled)      {}
func rv64GCBrOnCastConcrete(*Compiled) bool                        { return false }
func rv64GCBrOnCastConcreteExecutionCode(*Compiled) bool           { return false }
func rv64GCBrOnCastFailConcrete(*Compiled) bool                    { return false }
func rv64GCBrOnCastFailConcreteExecutionCode(*Compiled) bool       { return false }
func railshotRestoreGCBrOnCastConcreteArtifact(*Compiled)          {}
func railshotRestoreGCBrOnCastFailConcreteArtifact(*Compiled)      {}
func rv64GCBrOnCastNullability(*Compiled) bool                     { return false }
func rv64GCBrOnCastNullabilityExecutionCode(*Compiled) bool        { return false }
func rv64GCBrOnCastFailNullability(*Compiled) bool                 { return false }
func rv64GCBrOnCastFailNullabilityExecutionCode(*Compiled) bool    { return false }
func railshotRestoreGCBrOnCastNullabilityArtifact(*Compiled)       {}
func railshotRestoreGCBrOnCastFailNullabilityArtifact(*Compiled)   {}
func rv64GCTypeSubtypingDeclarations(*Compiled) bool               { return false }
func railshotRestoreGCTypeSubtypingDeclarationsArtifact(*Compiled) {}
func rv64GCTypeSubtypingRecursiveFunctions(*Compiled) bool         { return false }
func rv64GCTypeSubtypingRecursiveFunctionsExecutionCode(*Compiled) bool {
	return false
}
func railshotRestoreGCTypeSubtypingRecursiveFunctionsArtifact(*Compiled) {}
func rv64GCTypeSubtypingRefFuncGlobals(*Compiled) bool                   { return false }
func rv64GCTypeSubtypingRefFuncGlobalsExecutionCode(*Compiled) bool      { return false }
func railshotRestoreGCTypeSubtypingRefFuncGlobalsArtifact(*Compiled)     {}
func rv64GCTypeSubtypingRuntimeCallCast(*Compiled) bool                  { return false }
func rv64GCTypeSubtypingRuntimeCallCastExecutionCode(*Compiled) bool     { return false }
func railshotRestoreGCTypeSubtypingRuntimeCallCastArtifact(*Compiled)    {}

func rv64GCTypeSubtypingRuntimeFinalityCallCast(*Compiled) bool               { return false }
func rv64GCTypeSubtypingRuntimeFinalityCallCastExecutionCode(*Compiled) bool  { return false }
func railshotRestoreGCTypeSubtypingRuntimeFinalityCallCastArtifact(*Compiled) {}

func rv64GCTypeSubtypingRuntimeTypedTableCall(*Compiled) bool               { return false }
func rv64GCTypeSubtypingRuntimeTypedTableCallExecutionCode(*Compiled) bool  { return false }
func railshotRestoreGCTypeSubtypingRuntimeTypedTableCallArtifact(*Compiled) {}

func rv64GCTypeSubtypingRefTestSingle(*Compiled) bool               { return false }
func rv64GCTypeSubtypingRefTestSingleExecutionCode(*Compiled) bool  { return false }
func railshotRestoreGCTypeSubtypingRefTestSingleArtifact(*Compiled) {}

func railshotRestoreGCRefCastAbstractArtifact(*Compiled) {}
func rv64GCRefCastConcrete(*Compiled) bool               { return false }
func rv64GCRefCastConcreteExecutionCode(*Compiled) bool  { return false }
func railshotRestoreGCRefCastConcreteArtifact(*Compiled) {}
