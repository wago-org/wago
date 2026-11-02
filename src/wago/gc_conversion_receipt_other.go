//go:build !linux || riscv64 || tinygo || wago_target_tinygo

package wago

type gcConversionIdentity struct{}
type gcConversionCore struct{}
type gcConversionContainer struct{}

func bindGCConversionCore(*Instance) error                 { return nil }
func closeGCConversionCoreLocked(*gcExternConversionState) {}
func publishGCConversionTable(s *gcRefTestTableState, table, index, old, word uint64, publish func() error) error {
	return s.Conversion.publishExternReplacement(old, word, publish)
}
func releaseGCConversionTables(*gcRefTestTableState)                {}
func gcConversionCollectorRegistered(*gcExternConversionState) bool { return true }
func releaseGCConversionOwner(*Instance)                            {}
func lockGCConversionRetirement(*Instance) func()                   { return func() {} }
func retireGCConversionStoreLocked(*referenceStore)                 {}
