//go:build !linux || !riscv64 || tinygo || wago_target_tinygo

package wago

func rv64GCRefTestConcrete(*Compiled) bool              { return false }
func rv64GCRefTestConcreteExecutionCode(*Compiled) bool { return false }

func rv64GCRefTestAbstract(*Compiled) bool              { return false }
func rv64GCRefTestAbstractExecutionCode(*Compiled) bool { return false }

func rv64GCExtern(*Compiled) bool              { return false }
func rv64GCExternExecutionCode(*Compiled) bool { return false }

func rv64GCRefCastAbstract(*Compiled) bool                   { return false }
func rv64GCRefCastAbstractExecutionCode(*Compiled) bool      { return false }
func rv64GCBrOnCastAbstract(*Compiled) bool                  { return false }
func rv64GCBrOnCastAbstractExecutionCode(*Compiled) bool     { return false }
func rv64GCBrOnCastFailAbstract(*Compiled) bool              { return false }
func rv64GCBrOnCastFailAbstractExecutionCode(*Compiled) bool { return false }

func rv64GCBrOnCastConcrete(*Compiled) bool                  { return false }
func rv64GCBrOnCastConcreteExecutionCode(*Compiled) bool     { return false }
func rv64GCBrOnCastFailConcrete(*Compiled) bool              { return false }
func rv64GCBrOnCastFailConcreteExecutionCode(*Compiled) bool { return false }

func rv64GCBrOnCastNullability(*Compiled) bool                  { return false }
func rv64GCBrOnCastNullabilityExecutionCode(*Compiled) bool     { return false }
func rv64GCBrOnCastFailNullability(*Compiled) bool              { return false }
func rv64GCBrOnCastFailNullabilityExecutionCode(*Compiled) bool { return false }

func rv64GCTypeSubtypingDeclarations(*Compiled) bool { return false }

func rv64GCTypeSubtypingRecursiveFunctions(*Compiled) bool { return false }
func rv64GCTypeSubtypingRecursiveFunctionsExecutionCode(*Compiled) bool {
	return false
}

func rv64GCTypeSubtypingRefFuncGlobals(*Compiled) bool              { return false }
func rv64GCTypeSubtypingRefFuncGlobalsExecutionCode(*Compiled) bool { return false }

func rv64GCTypeSubtypingRuntimeCallCast(*Compiled) bool              { return false }
func rv64GCTypeSubtypingRuntimeCallCastExecutionCode(*Compiled) bool { return false }

func rv64GCTypeSubtypingRuntimeFinalityCallCast(*Compiled) bool              { return false }
func rv64GCTypeSubtypingRuntimeFinalityCallCastExecutionCode(*Compiled) bool { return false }

func rv64GCTypeSubtypingRuntimeTypedTableCall(*Compiled) bool              { return false }
func rv64GCTypeSubtypingRuntimeTypedTableCallExecutionCode(*Compiled) bool { return false }

func rv64GCTypeSubtypingRefTestSingle(*Compiled) bool              { return false }
func rv64GCTypeSubtypingRefTestSingleExecutionCode(*Compiled) bool { return false }

func rv64GCTypeSubtypingLink(*Compiled) bool              { return false }
func rv64GCTypeSubtypingLinkExecutionCode(*Compiled) bool { return false }

func rv64GCTypeSubtypingFinalityLink(*Compiled) bool              { return false }
func rv64GCTypeSubtypingFinalityLinkExecutionCode(*Compiled) bool { return false }

func rv64GCTypeSubtypingStructLink(*Compiled) bool              { return false }
func rv64GCTypeSubtypingStructLinkExecutionCode(*Compiled) bool { return false }

func rv64GCTypeSubtypingStructProjectionLink(*Compiled) bool { return false }
func rv64GCTypeSubtypingStructProjectionLinkExecutionCode(*Compiled) bool {
	return false
}

func rv64GCTypeSubtypingStructMismatchLink(*Compiled) bool              { return false }
func rv64GCTypeSubtypingStructMismatchLinkExecutionCode(*Compiled) bool { return false }

func rv64GCTypeSubtypingIndependentStructLink(*Compiled) bool              { return false }
func rv64GCTypeSubtypingIndependentStructLinkExecutionCode(*Compiled) bool { return false }

func rv64GCTypeSubtypingExtendedProjectionLink(*Compiled) bool { return false }
func rv64GCTypeSubtypingExtendedProjectionLinkExecutionCode(*Compiled) bool {
	return false
}

func rv64GCTypeSubtypingDuplicateRecursiveLink(*Compiled) bool              { return false }
func rv64GCTypeSubtypingDuplicateRecursiveLinkExecutionCode(*Compiled) bool { return false }

func rv64GCTypeSubtypingExtendedRecursiveLink(*Compiled) bool              { return false }
func rv64GCTypeSubtypingExtendedRecursiveLinkExecutionCode(*Compiled) bool { return false }

func rv64GCTypeSubtypingCrossGroupMismatchLink(*Compiled) bool { return false }
func rv64GCTypeSubtypingCrossGroupMismatchLinkExecutionCode(*Compiled) bool {
	return false
}

func rv64GCTypeSubtypingTransitiveMismatchLink(*Compiled) bool { return false }
func rv64GCTypeSubtypingTransitiveMismatchLinkExecutionCode(*Compiled) bool {
	return false
}

func rv64GCTypeSubtypingNonFlatExport(*Compiled) bool { return false }
func rv64GCTypeSubtypingNonFlatExportExecutionCode(*Compiled) bool {
	return false
}

func rv64GCRefCastConcrete(*Compiled) bool              { return false }
func rv64GCRefCastConcreteExecutionCode(*Compiled) bool { return false }
