package shared

// LocalTraffic attributes local/frame memory instructions to the compiler
// decision that emitted them. It is fixed-size and lives only in opt-in
// CodegenStats; ordinary compilation does not allocate or retain it.
type LocalTraffic struct {
	ParameterHomeStores     int
	DeclaredLocalZeroStores int
	OrdinarySpillStores     int
	OrdinarySpillReloads    int
	ControlMergeStores      int
	ControlMergeReloads     int
	CallPreservationStores  int
	CallPreservationReloads int
}

// Any reports whether the function emitted attributed local/frame traffic.
func (t LocalTraffic) Any() bool {
	return t.ParameterHomeStores != 0 || t.DeclaredLocalZeroStores != 0 ||
		t.OrdinarySpillStores != 0 || t.OrdinarySpillReloads != 0 ||
		t.ControlMergeStores != 0 || t.ControlMergeReloads != 0 ||
		t.CallPreservationStores != 0 || t.CallPreservationReloads != 0
}

// CallTraffic attributes physical register-copy instructions emitted solely to
// satisfy the internal register ABI. Loads, constant materialization, wrapper
// slot traffic, and ordinary allocator copies remain outside this first slice.
// The fixed-size counters live only in opt-in CodegenStats.
type CallTraffic struct {
	RegisterArgumentMoves int
	RegisterResultMoves   int

	// AMD64 argument moves are split by the lowering path that requested them.
	// The total above remains the cross-target headline counter.
	IntegerCallArgumentMoves int
	MixedCallArgumentMoves   int
	TailCallArgumentMoves    int
}

func (t CallTraffic) Any() bool {
	return t.RegisterArgumentMoves != 0 || t.RegisterResultMoves != 0 ||
		t.IntegerCallArgumentMoves != 0 || t.MixedCallArgumentMoves != 0 ||
		t.TailCallArgumentMoves != 0
}
