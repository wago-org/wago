//go:build (!amd64 && !arm64) || (tinygo && amd64 && !linux) || (arm64 && !(linux || darwin || windows)) || (tinygo && arm64 && windows)

package wago

import "fmt"

const preparedDirectIntSupported = false
const preparedDirectIntPrivateSupported = false
const preparedDirectFPSupported = false

func (fn *PreparedFunction) invokeDirectInt([]uint64) ([]uint64, error) {
	return nil, fmt.Errorf("wago: direct prepared integer entry is unavailable on this architecture")
}

func (fn *PreparedFunction) invokeDirectIntFixed(uint64, uint64, uint64, uint64) ([]uint64, error) {
	return nil, fmt.Errorf("wago: direct prepared integer entry is unavailable on this architecture")
}

func (fn *PreparedFunction) invokeDirectFP([]uint64) ([]uint64, error) {
	return nil, fmt.Errorf("wago: direct prepared FP entry is unavailable on this architecture")
}

func (fn *PreparedFunction) invokeDirectPair([]uint64) ([]uint64, error) {
	return nil, fmt.Errorf("wago: direct prepared result-pair entry is unavailable on this architecture")
}

func (fn *PreparedFunction) invokeDirectMixed([]uint64) ([]uint64, error) {
	return nil, fmt.Errorf("wago: direct prepared mixed entry is unavailable on this architecture")
}
