//go:build (!amd64 && !arm64) || (tinygo && amd64 && !linux) || (arm64 && !(linux || darwin || windows)) || (tinygo && arm64 && windows)

package wago

import "fmt"

const preparedDirectIntSupported = false
const preparedDirectIntPrivateSupported = false

func (fn *PreparedFunction) invokeDirectInt([]uint64) ([]uint64, error) {
	return nil, fmt.Errorf("wago: direct prepared integer entry is unavailable on this architecture")
}

func (fn *PreparedFunction) invokeDirectIntFixed(uint64, uint64, uint64, uint64) ([]uint64, error) {
	return nil, fmt.Errorf("wago: direct prepared integer entry is unavailable on this architecture")
}

func (in *Instance) invokeDirectIntEntry(uintptr, int, int, uint8, bool, bool, uint64, uint64, uint64, uint64) ([]uint64, error) {
	return nil, fmt.Errorf("wago: direct prepared integer entry is unavailable on this architecture")
}
