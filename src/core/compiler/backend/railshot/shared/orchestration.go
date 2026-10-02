package shared

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

// FirstErrorIndex returns the first function error in source order. Parallel
// compilers use it after all workers join so diagnostics never depend on
// scheduling order.
func FirstErrorIndex(functions int, errorAt func(int) error) (int, error) {
	for i := 0; i < functions; i++ {
		if err := errorAt(i); err != nil {
			return i, err
		}
	}
	return 0, nil
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
