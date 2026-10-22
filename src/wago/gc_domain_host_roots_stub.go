//go:build !linux || !riscv64

package wago

type gcDomainHostRootScratch struct{}

func (*gcDomainHostRootScratch) suspendedOwnerScratch() []gcSuspendedRootOwner {
	return nil
}

func (*gcDomainHostRootScratch) retainSuspendedOwnerScratch([]gcSuspendedRootOwner) {}
