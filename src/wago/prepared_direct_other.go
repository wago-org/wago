//go:build !amd64 || tinygo

package wago

import "fmt"

func (fn *PreparedFunction) invokeDirectInt([]uint64) ([]uint64, error) {
	return nil, fmt.Errorf("wago: direct prepared integer entry is unavailable on this architecture")
}
