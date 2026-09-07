package shared

// ResidencyStats records the physical traffic around regional local leases.
// It is deliberately pointer-free so opt-in explain collection does not add
// another scannable object graph to compilation.
type ResidencyStats struct {
	Candidates      int
	Activations     int
	ActivationLoads int
	PressureMisses  int
	Evictions       int
	DirtyWritebacks int
	FinalTransfers  int
	MaxActive       int
}

// Active reports whether any residency event was observed.
func (s ResidencyStats) Active() bool {
	return s.Candidates|s.Activations|s.ActivationLoads|s.PressureMisses|
		s.Evictions|s.DirtyWritebacks|s.FinalTransfers|s.MaxActive != 0
}
