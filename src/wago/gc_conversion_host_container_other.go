//go:build !linux || riscv64 || tinygo || wago_target_tinygo

package wago

func registerConversionHost(*referenceStore, []byte, *tableOwner, *globalOwner) {}
func lockConversionHostTableClose(*Table) func()                                { return func() {} }
func lockConversionHostGlobalClose(*Global) func()                              { return func() {} }
func releaseConversionHostStorage(*referenceStore, []byte)                      {}
func conversionHostPublicWord(*referenceStore, uint64) (uint64, bool)           { return 0, false }
func conversionHostRootIsStrong(*gcConversionContainer) bool                    { return true }
