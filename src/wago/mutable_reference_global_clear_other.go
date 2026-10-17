//go:build !linux || !riscv64 || tinygo || wago_target_tinygo

package wago

func railshotUsesGuestGlobalStoreHelper(*Compiled) bool { return false }

func (*Instance) retainGuestGlobalStoreDescriptor(uint32, []uint64, []uint64) (*Instance, error) {
	return nil, nil
}

func (*Instance) dispatchGuestGlobalStore(uint32, []uint64, []uint64) bool { return false }

func (*Instance) dispatchGuestGlobalStoreWithOwner(uint32, []uint64, []uint64, *Instance) bool {
	return false
}
