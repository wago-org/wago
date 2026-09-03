package shared

import "sync"

// ResolveWorkers caps a requested per-function worker count to the process and
// module limits. Values <= 1 preserve the serial fast path.
func ResolveWorkers(requested, functions, gomaxprocs int) int {
	if requested <= 1 || functions <= 1 {
		return 1
	}
	if gomaxprocs < 1 {
		gomaxprocs = 1
	}
	if requested > gomaxprocs {
		requested = gomaxprocs
	}
	if requested > functions {
		requested = functions
	}
	return requested
}

// PressureThreshold returns the explicit output threshold, or seven eighths of
// the estimated final code capacity when no threshold was supplied.
func PressureThreshold(explicit, codeCapacity int) int {
	if explicit > 0 {
		return explicit
	}
	return codeCapacity * 7 / 8
}

// ModuleEntries allocates the public and internal function-entry tables in one
// exact backing store. Full-slice bounds keep the tables independently
// appendable: growing either table cannot overwrite the other half.
func ModuleEntries(functions int) (entry, internal []int) {
	if functions > int(^uint(0)>>1)/2 {
		// Preserve the former independent-allocation behavior if a caller somehow
		// reaches a length that cannot be doubled in one slice header.
		return make([]int, functions), make([]int, functions)
	}
	entries := make([]int, 2*functions)
	return entries[:functions:functions], entries[functions:]
}

// LowestIndexError retains only the lowest-indexed function error produced by
// parallel compilation. Keeping this once per module avoids an error interface
// in every per-function result while preserving deterministic diagnostics.
type LowestIndexError struct {
	mu    sync.Mutex
	index int
	err   error
}

func (e *LowestIndexError) Reset(limit int) {
	e.index, e.err = limit, nil
}

func (e *LowestIndexError) Record(index int, err error) {
	if err == nil {
		return
	}
	e.mu.Lock()
	if index < e.index {
		e.index, e.err = index, err
	}
	e.mu.Unlock()
}

func (e *LowestIndexError) Result() (int, error) {
	e.mu.Lock()
	index, err := e.index, e.err
	e.mu.Unlock()
	return index, err
}

// ModuleGlobalPinInfo is the architecture-neutral display form of one
// module-wide global-to-register reservation.
type ModuleGlobalPinInfo struct {
	Global uint32
	Reg    string
}

const (
	CallInline         = "inline"
	CallHost           = "host"
	CallHostSync       = "hostsync"
	CallCrossInstance  = "crossinstance"
	CallImportDispatch = "importdispatch"
	CallRegisterABI    = "regabi"
	CallMixed          = "mixed"
	CallWrapper        = "wrapper"
	CallIndirect       = "indirect"
)
