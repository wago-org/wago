//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

func conversionAddClaimedDomainsLocked(*referenceStore, *gcInvocationDomainSet) {}
func clearUnreachableConversionRoots(*managedFunctionMarkDomain) error          { return nil }
func drainConversionClaims(*managedFunctionGraph)                               {}
