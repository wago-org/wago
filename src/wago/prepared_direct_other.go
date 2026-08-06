//go:build !amd64 || tinygo

package wago

import "fmt"

func (in *Instance) callPreparedDirectInt(uintptr, []uint64, uint8, []byte) (uint64, error) {
	return 0, fmt.Errorf("wago: direct prepared integer entry is unavailable on this architecture")
}
