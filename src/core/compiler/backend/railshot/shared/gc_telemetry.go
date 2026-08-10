package shared

// GCNativeCodeBytes attributes emitted code to stable WasmGC families. Fields
// are diagnostic and may overlap when one sequence serves multiple purposes;
// TotalBytes remains the authoritative emitted function size.
type GCNativeCodeBytes struct {
	Total            int
	Allocation       int
	HandleResolution int
	TypeCast         int
	NullCheck        int
	BoundsCheck      int
	Barrier          int
	SpillReload      int
	HelperCall       int
	SharedStub       int
	TrapStub         int
	RootMap          int
}

func (s *GCNativeCodeBytes) Add(other GCNativeCodeBytes) {
	if s == nil {
		return
	}
	s.Total += other.Total
	s.Allocation += other.Allocation
	s.HandleResolution += other.HandleResolution
	s.TypeCast += other.TypeCast
	s.NullCheck += other.NullCheck
	s.BoundsCheck += other.BoundsCheck
	s.Barrier += other.Barrier
	s.SpillReload += other.SpillReload
	s.HelperCall += other.HelperCall
	s.SharedStub += other.SharedStub
	s.TrapStub += other.TrapStub
	s.RootMap += other.RootMap
}
