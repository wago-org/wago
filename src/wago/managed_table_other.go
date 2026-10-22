//go:build !linux || !riscv64 || tinygo || wago_target_tinygo

package wago

func railshotManagedVoidTableEntry(*Instance, uint32) (uintptr, bool, error) {
	return 0, false, nil
}

func railshotManagedTableDescriptor(in *Instance) []byte {
	return in.tableDescriptor(0)
}
