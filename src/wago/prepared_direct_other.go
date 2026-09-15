//go:build (!amd64 && !arm64) || (tinygo && amd64 && !linux) || (arm64 && !(linux || darwin))

package wago

import "fmt"

const preparedDirectIntSupported = false
const preparedDirectIntPrivateSupported = false
const preparedIntCallBlockDefault = false

func (fn *PreparedFunction) initDirectIntCall() {}

func (fn *PreparedFunction) invokeDirectInt([]uint64) ([]uint64, error) {
	return nil, fmt.Errorf("wago: direct prepared integer entry is unavailable on this architecture")
}

func (fn *PreparedFunction) invokeDirectIntFixed(uint64, uint64, uint64, uint64) ([]uint64, error) {
	return nil, fmt.Errorf("wago: direct prepared integer entry is unavailable on this architecture")
}

func (fn *PreparedFunction) invokeDirectTrapIntFixed(a0, a1, a2, a3 uint64) ([]uint64, error) {
	return fn.invokeDirectIntFixed(a0, a1, a2, a3)
}

func (in *Instance) invokeDirectIntEntry(uintptr, int, int, uint8, bool, bool, bool, bool, uint64, uint64, uint64, uint64) ([]uint64, error) {
	return nil, fmt.Errorf("wago: direct prepared integer entry is unavailable on this architecture")
}

func (fn *PreparedFunction) invokeDirectIntSession(a0, a1, a2, a3 uint64) ([]uint64, error) {
	return fn.in.invokeDirectIntEntry(
		fn.directEntry, fn.paramSlots, fn.resultSlots, fn.scalarWideMask,
		fn.scalarResultWide, fn.directIsolated, fn.directIntLight, fn.directIntBounded,
		a0, a1, a2, a3,
	)
}
