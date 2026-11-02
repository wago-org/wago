//go:build !linux || riscv64 || tinygo || wago_target_tinygo

package wago

func compiledConversionStoreHooks(*Compiled) bool { return false }
func (*Instance) publishConversionStore(_ *Instance, _ []byte, _, _ uint64, publish func() error) error {
	return publish()
}

func conversionImportedTableCount(*gcRefTestTableState) int { return 0 }

func conversionBoundaryWord(*Instance, uint64, bool) (uint64, bool)     { return 0, false }
func setConversionGlobalPublic(*Instance, *Global, Value) (bool, error) { return false, nil }

func (*Instance) publishConversionRange(_ *Instance, _ []byte, _, _ uint64, _ []uint64, publish func()) error {
	publish()
	return nil
}
