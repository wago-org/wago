//go:build linux && riscv64

package wago

// Only RV64 root sets enumerate older same-domain host activations. Reuse cold
// overflow storage without adding unused state bytes on the other targets.
type gcDomainHostRootScratch struct {
	domainHostOwnersExtra []gcSuspendedRootOwner
}

func (s *gcDomainHostRootScratch) suspendedOwnerScratch() []gcSuspendedRootOwner {
	return s.domainHostOwnersExtra[:0]
}

func (s *gcDomainHostRootScratch) retainSuspendedOwnerScratch(owners []gcSuspendedRootOwner) {
	s.domainHostOwnersExtra = owners[:0]
}
